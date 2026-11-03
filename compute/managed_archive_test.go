package compute_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type archiveTestMember struct {
	name string
	data []byte
	link bool
}

func archiveTestBytes(t *testing.T, format string, members []archiveTestMember, stored bool) []byte {
	t.Helper()
	var out bytes.Buffer
	if format == "zip" {
		w := zip.NewWriter(&out)
		if err := w.SetComment("verified archive"); err != nil {
			t.Fatal(err)
		}
		for _, member := range members {
			header := &zip.FileHeader{Name: member.name, Method: zip.Deflate}
			if stored {
				header.Method = zip.Store
			}
			header.SetMode(0600)
			if member.link {
				header.SetMode(os.ModeSymlink | 0777)
			}
			file, err := w.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = file.Write(member.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	var target io.Writer = &out
	var compressed *gzip.Writer
	if format == "tar.gz" {
		compressed = gzip.NewWriter(&out)
		target = compressed
	}
	w := tar.NewWriter(target)
	for _, member := range members {
		header := &tar.Header{Name: member.name, Mode: 0600, Size: int64(len(member.data)), Typeflag: tar.TypeReg}
		if member.link {
			header.Typeflag = tar.TypeSymlink
			header.Size = 0
			header.Linkname = "other.img"
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if !member.link {
			if _, err := w.Write(member.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if compressed != nil {
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

type archivePayloadBackend struct {
	*imageBackend
	payloadMu sync.Mutex
	payloads  map[string][]byte
}

func (b *archivePayloadBackend) UploadImage(ctx context.Context, binding compute.ImageBinding, r io.Reader, size uint64) error {
	var payload bytes.Buffer
	if err := b.imageBackend.UploadImage(ctx, binding, io.TeeReader(r, &payload), size); err != nil {
		return err
	}
	b.payloadMu.Lock()
	defer b.payloadMu.Unlock()
	b.payloads[binding.Path] = append([]byte(nil), payload.Bytes()...)
	return nil
}

func archiveService(t *testing.T, url string, data []byte, format, member string) (*compute.ManagedImageService, *archivePayloadBackend, compute.Image) {
	t.Helper()
	_, base, repo, image := imageSetup(t, url, data)
	backend := &archivePayloadBackend{imageBackend: base, payloads: map[string][]byte{}}
	service := compute.NewManagedImageService(backend, repo, map[string]compute.ImageBinding{"one": {Scope: "backend:one", Pool: "images-one"}}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: 5 * time.Second, IdleTimeout: time.Second})
	image.Download.ArchiveFormat = format
	image.Download.ArchiveMember = member
	image.Download.SizeBytes = uint64(len(data))
	return service, backend, image
}

func TestManagedArchiveSelectionAndReuse(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			first := []byte(strings.Repeat("first disk contents", 512))
			second := []byte(strings.Repeat("second disk contents", 512))
			archive := archiveTestBytes(t, format, []archiveTestMember{{"bundle/first.img", first, false}, {"bundle/second.img", second, false}}, false)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
				w.Write(archive)
			}))
			defer server.Close()
			images, backend, image := archiveService(t, server.URL, archive, format, "bundle/first.img")
			image.Download.ExtractedSizeBytes = uint64(len(first))
			binding, err := images.Bind("one", image)
			if err != nil {
				t.Fatal(err)
			}
			if state, _ := images.Status(binding, image); state != "downloadable" || requests.Load() != 0 {
				t.Fatalf("discovery fetched archive: %s %d", state, requests.Load())
			}
			s, _ := imageTasks(t, images)
			task := submitImage(t, s, images, image, "one", "archive-first")
			if result := waitImage(t, s, task.ID); result.State != tasks.Succeeded {
				t.Fatalf("%+v", result)
			}
			backend.payloadMu.Lock()
			got := backend.payloads[binding.Path]
			backend.payloadMu.Unlock()
			if !bytes.Equal(got, first) {
				t.Fatal("upload included archive bytes or selected the wrong member")
			}
			volume, _, err := backend.InspectImage(binding)
			if err != nil || volume.Size.Bytes() != uint64(len(first)) {
				t.Fatalf("allocation used outer archive size: %+v %v", volume, err)
			}
			perDownload := int32(1)
			if format == "zip" {
				perDownload = 2
			}
			if requests.Load() != perDownload {
				t.Fatalf("unexpected HTTP requests: %d", requests.Load())
			}
			reused := submitImage(t, s, images, image, "one", "archive-reuse")
			if result := waitImage(t, s, reused.ID); result.State != tasks.Succeeded {
				t.Fatal(result)
			}
			if requests.Load() != perDownload || backend.creates != 1 {
				t.Fatal("verified archive member was downloaded again")
			}
			other := image
			download := *image.Download
			other.Download = &download
			other.Download.ArchiveMember = "bundle/second.img"
			other.Download.ExtractedSizeBytes = uint64(len(second))
			otherBinding, err := images.Bind("one", other)
			if err != nil || otherBinding.Path == binding.Path {
				t.Fatal("archive member omitted from cache identity", err)
			}
			otherTask := submitImage(t, s, images, other, "one", "other-member")
			// Accepted tasks contain the selected member, independently of caller edits.
			other.Download.ArchiveMember = "changed-after-acceptance.img"
			if result := waitImage(t, s, otherTask.ID); result.State != tasks.Succeeded {
				t.Fatal(result)
			}
			backend.payloadMu.Lock()
			got = backend.payloads[otherBinding.Path]
			backend.payloadMu.Unlock()
			if !bytes.Equal(got, second) || requests.Load() != 2*perDownload || backend.creates != 2 {
				t.Fatal("member identity or accepted selection was not preserved")
			}
		})
	}
}

func TestManagedArchiveFailuresDoNotPublishImages(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		for _, failure := range []string{"missing", "duplicate", "symlink", "checksum", "size", "expanded-limit", "upload"} {
			t.Run(format+"/"+failure, func(t *testing.T) {
				member := archiveTestMember{"disk.img", []byte(strings.Repeat("disk data", 1024)), false}
				if failure == "symlink" {
					member.link = true
				}
				if failure == "expanded-limit" {
					member.data = bytes.Repeat([]byte{0x42}, (4<<20)+1)
				}
				members := []archiveTestMember{member}
				if failure == "duplicate" {
					members = append(members, member)
				}
				archive := archiveTestBytes(t, format, members, false)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
					w.Write(archive)
				}))
				defer server.Close()
				images, backend, image := archiveService(t, server.URL, archive, format, "disk.img")
				switch failure {
				case "missing":
					image.Download.ArchiveMember = "missing.img"
				case "checksum":
					image.Download.SHA256 = strings.Repeat("0", 64)
				case "size":
					image.Download.ExtractedSizeBytes = uint64(len(member.data) + 1)
				case "upload":
					backend.uploadFail = true
				}
				s, _ := imageTasks(t, images)
				task := submitImage(t, s, images, image, "one", "archive-failure")
				result := waitImage(t, s, task.ID)
				if result.State != tasks.Failed {
					t.Fatalf("invalid archive succeeded: %+v", result)
				}
				binding, _ := images.Bind("one", image)
				if state, _ := images.Status(binding, image); state == "ready" {
					t.Fatal("failed extraction was published")
				}
				if backend.creates != backend.deletes {
					t.Fatalf("owned staging was not removed: created %d deleted %d", backend.creates, backend.deletes)
				}
			})
		}
	}
}

func TestManagedZipVerifiesSecondResponseAndMemberCRC(t *testing.T) {
	for _, failure := range []string{"changed-second-response", "member-crc"} {
		t.Run(failure, func(t *testing.T) {
			archive := archiveTestBytes(t, "zip", []archiveTestMember{{"disk.img", []byte(strings.Repeat("disk content", 1024)), false}}, true)
			if failure == "member-crc" {
				reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
				if err != nil {
					t.Fatal(err)
				}
				offset, err := reader.File[0].DataOffset()
				if err != nil {
					t.Fatal(err)
				}
				archive[offset+10] ^= 1
			}
			changed := append([]byte(nil), archive...)
			changed[len(changed)-1] ^= 1 // Archive comment changes, while the selected file stays valid.
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := requests.Add(1)
				payload := archive
				if failure == "changed-second-response" && count > 1 {
					payload = changed
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
				w.Write(payload)
			}))
			defer server.Close()
			images, backend, image := archiveService(t, server.URL, archive, "zip", "disk.img")
			s, _ := imageTasks(t, images)
			task := submitImage(t, s, images, image, "one", failure)
			if result := waitImage(t, s, task.ID); result.State != tasks.Failed {
				t.Fatalf("archive integrity failure accepted: %+v", result)
			}
			binding, _ := images.Bind("one", image)
			if state, _ := images.Status(binding, image); state == "ready" || backend.creates != backend.deletes {
				t.Fatal("integrity failure left a ready image or staging")
			}
		})
	}
}

func TestManagedTarValidatesBeyondSelectedMember(t *testing.T) {
	for _, format := range []string{"tar", "tar.gz"} {
		for _, failure := range []string{"trailing-checksum", "trailing-format", "skipped-expansion", "concatenated-tar"} {
			t.Run(format+"/"+failure, func(t *testing.T) {
				members := []archiveTestMember{{"disk.img", []byte(strings.Repeat("selected data", 1024)), false}, {"later.img", []byte("other data"), false}}
				if failure == "skipped-expansion" {
					members = []archiveTestMember{{"skip.img", bytes.Repeat([]byte{0x42}, (4<<20)+1), false}, members[0]}
				}
				archive := archiveTestBytes(t, format, members, false)
				payload := append([]byte(nil), archive...)
				if failure == "concatenated-tar" {
					duplicate := archiveTestBytes(t, format, []archiveTestMember{members[0]}, false)
					payload = append(payload, duplicate...)
					archive = payload
				}
				if failure == "trailing-checksum" {
					payload = append(payload, 'X')
				} else if failure == "trailing-format" {
					if format == "tar.gz" {
						payload[len(payload)-8] ^= 1 // Invalid gzip CRC, covered by the outer SHA below.
					} else {
						nextHeader := 512 + ((len(members[0].data)+511)/512)*512
						payload[nextHeader+148] ^= 0x40 // Invalid TAR checksum after the selected file.
					}
					archive = payload
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
					w.Write(payload)
				}))
				defer server.Close()
				images, backend, image := archiveService(t, server.URL, archive, format, "disk.img")
				if failure == "trailing-checksum" {
					image.Download.SizeBytes = uint64(len(payload))
				}
				s, _ := imageTasks(t, images)
				task := submitImage(t, s, images, image, "one", failure)
				if result := waitImage(t, s, task.ID); result.State != tasks.Failed {
					t.Fatalf("archive tail accepted: %+v", result)
				}
				binding, _ := images.Bind("one", image)
				if state, _ := images.Status(binding, image); state == "ready" || backend.creates != backend.deletes {
					t.Fatal("invalid archive tail left a ready image or staging")
				}
			})
		}
	}
}

func TestManagedZipSecondRequestFailure(t *testing.T) {
	archive := archiveTestBytes(t, "zip", []archiveTestMember{{"disk.img", []byte(strings.Repeat("disk", 1024)), false}}, false)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) > 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
		w.Write(archive)
	}))
	defer server.Close()
	images, backend, image := archiveService(t, server.URL, archive, "zip", "disk.img")
	s, _ := imageTasks(t, images)
	task := submitImage(t, s, images, image, "one", "second-request-failure")
	if result := waitImage(t, s, task.ID); result.State != tasks.Failed {
		t.Fatalf("HTTP error not reported: %+v", result)
	}
	if backend.creates != 0 || backend.uploads != 0 {
		t.Fatal("second HTTP request failure mutated backend storage")
	}
}

func TestManagedZipCachedPayloadProgressDoesNotTimeOut(t *testing.T) {
	payload := bytes.Repeat([]byte{0x42}, 1<<20)
	archive := archiveTestBytes(t, "zip", []archiveTestMember{{"disk.img", payload, false}}, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
		w.Write(archive)
	}))
	defer server.Close()
	_, base, repo, image := imageSetup(t, server.URL, archive)
	backend := &archivePayloadBackend{imageBackend: base, payloads: map[string][]byte{}}
	images := compute.NewManagedImageService(backend, repo, map[string]compute.ImageBinding{"one": {Scope: "backend:one", Pool: "images-one"}}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: 5 * time.Second, IdleTimeout: 100 * time.Millisecond})
	image.Download.ArchiveFormat = "zip"
	image.Download.ArchiveMember = "disk.img"
	s, _ := imageTasks(t, images)
	task := submitImage(t, s, images, image, "one", "cached-payload-progress")
	// The compressed payload fits in the metadata tail, so selected reads do not
	// touch the second HTTP response. The fake upload takes >250 ms while making
	// steady progress, which must reset the 100 ms idle timer.
	if result := waitImage(t, s, task.ID); result.State != tasks.Succeeded {
		t.Fatalf("productive cached extraction timed out: %+v", result)
	}
}

func TestManagedArchiveTruncatedHTTPResponse(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			archive := archiveTestBytes(t, format, []archiveTestMember{{"disk.img", []byte(strings.Repeat("disk", 1024)), false}}, false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
				w.Write(archive[:len(archive)-10])
			}))
			defer server.Close()
			images, backend, image := archiveService(t, server.URL, archive, format, "disk.img")
			s, _ := imageTasks(t, images)
			task := submitImage(t, s, images, image, "one", "truncated-archive-response")
			if result := waitImage(t, s, task.ID); result.State != tasks.Failed {
				t.Fatalf("truncated response accepted: %+v", result)
			}
			binding, _ := images.Bind("one", image)
			if state, _ := images.Status(binding, image); state == "ready" || backend.creates != backend.deletes {
				t.Fatal("truncated response published or left staging")
			}
		})
	}
}

type shortArchiveUploadBackend struct {
	*imageBackend
	consume int64
}

func (b *shortArchiveUploadBackend) UploadImage(_ context.Context, _ compute.ImageBinding, reader io.Reader, _ uint64) error {
	b.mu.Lock()
	b.uploads++
	b.mu.Unlock()
	if b.consume > 0 {
		_, err := io.CopyN(io.Discard, reader, b.consume)
		return err
	}
	return nil
}

func TestManagedArchiveRequiresCompleteBackendConsumption(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		for _, consumed := range []int64{0, 1} {
			t.Run(fmt.Sprintf("%s/%d", format, consumed), func(t *testing.T) {
				// This entire member fits in source header inspection. Verification
				// before upload must not imply that the backend consumed those bytes.
				archive := archiveTestBytes(t, format, []archiveTestMember{{"disk.img", []byte("tiny disk"), false}}, false)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Length", fmt.Sprint(len(archive)))
					w.Write(archive)
				}))
				defer server.Close()
				_, base, repo, image := imageSetup(t, server.URL, archive)
				backend := &shortArchiveUploadBackend{imageBackend: base, consume: consumed}
				images := compute.NewManagedImageService(backend, repo, map[string]compute.ImageBinding{"one": {Scope: "backend:one", Pool: "images-one"}}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: 5 * time.Second, IdleTimeout: time.Second})
				image.Download.ArchiveFormat = format
				image.Download.ArchiveMember = "disk.img"
				s, _ := imageTasks(t, images)
				task := submitImage(t, s, images, image, "one", "incomplete-backend-upload")
				result := waitImage(t, s, task.ID)
				if result.State != tasks.Failed || !strings.Contains(result.Error.Message, "consume") {
					t.Fatalf("backend accepted incomplete upload: %+v", result)
				}
				binding, _ := images.Bind("one", image)
				if state, _ := images.Status(binding, image); state == "ready" || backend.creates != 1 || backend.deletes != 1 || backend.uploads != 1 {
					t.Fatalf("incomplete upload was published or not cleaned: state %s, creates %d, deletes %d, uploads %d", state, backend.creates, backend.deletes, backend.uploads)
				}
			})
		}
	}
}
