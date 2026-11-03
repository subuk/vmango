package compute

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"subuk/vmango/tasks"
	"time"
)

// Archive downloads retain only bounded metadata in memory. The selected member
// reaches EOF only after the entire HTTP artifact has passed SHA-256 verification.
type archiveImageSource struct {
	reader                    io.Reader
	size, downloadSize, count uint64
	finish                    func() error
	close                     func() error
	complete                  bool
	ctx                       context.Context
	heartbeat                 func()
	startTransfer             func()
}

func (s *archiveImageSource) Close() error { return s.close() }
func (s *archiveImageSource) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if s.complete {
		return 0, io.EOF
	}
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	s.heartbeat()
	n, err := s.reader.Read(p)
	s.count += uint64(n)
	if s.count > s.size {
		return n, errors.New("archive member exceeds declared size")
	}
	if err == io.EOF {
		if s.count != s.size {
			return n, io.ErrUnexpectedEOF
		}
		if err = s.finish(); err != nil {
			return n, err
		}
		s.complete = true
		return n, io.EOF
	}
	return n, err
}

type verifiedDownload struct {
	*verifiedImageReader
	body io.ReadCloser
}

func (d *verifiedDownload) Close() error { d.timer.Stop(); return d.body.Close() }
func (s *ManagedImageService) openVerifiedDownload(ctx context.Context, cancel context.CancelFunc, i Image, e *tasks.Execution) (*verifiedDownload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.Download.URL, nil)
	if err != nil {
		return nil, errors.New("invalid source URL")
	}
	req.Header.Set("Accept-Encoding", "identity")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("image HTTP request failed; check source connectivity and timeout")
	}
	fail := func(err error) (*verifiedDownload, error) { response.Body.Close(); return nil, err }
	if response.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("image source returned HTTP %d", response.StatusCode))
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return fail(fmt.Errorf("image source HTTP encoding %q is unsupported", encoding))
	}
	var size uint64
	if response.ContentLength > 0 {
		size = uint64(response.ContentLength)
	}
	if configured := i.Download.SizeBytes; configured > 0 {
		if size > 0 && size != configured {
			return fail(errors.New("source_size_bytes disagrees with HTTP Content-Length"))
		}
		size = configured
	}
	if size == 0 {
		return fail(fmt.Errorf("cannot determine artifact byte size; set source_size_bytes on image %q", i.ID))
	}
	if size > s.limits.MaxBytes {
		return fail(fmt.Errorf("image exceeds image_transfers.max_bytes (%d)", s.limits.MaxBytes))
	}
	r := &verifiedImageReader{reader: response.Body, hash: sha256.New(), expected: i.Download.SHA256, size: size, progress: e, start: time.Now(), idle: s.limits.IdleTimeout}
	r.timer = time.AfterFunc(r.idle, cancel)
	return &verifiedDownload{verifiedImageReader: r, body: response.Body}, nil
}

func (s *ManagedImageService) openArchiveImage(ctx context.Context, cancel context.CancelFunc, i Image, e *tasks.Execution) (out *archiveImageSource, err error) {
	if !validArchiveMemberName(i.Download.ArchiveMember) {
		return nil, errors.New("invalid archive member path")
	}
	download, err := s.openVerifiedDownload(ctx, cancel, i, e)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && download != nil {
			download.Close()
		}
		if out != nil {
			out.ctx = ctx
			out.startTransfer = func() { download.progress = e }
			out.heartbeat = func() { download.timer.Reset(download.idle) }
		}
	}()
	downloadSize := download.size
	validateSize := func(size uint64) error {
		if size == 0 || size > s.limits.MaxBytes {
			return fmt.Errorf("archive member size must be positive and within image_transfers.max_bytes (%d)", s.limits.MaxBytes)
		}
		if want := i.Download.ExtractedSizeBytes; want != 0 && size != want {
			return errors.New("extracted_size_bytes disagrees with archive member size")
		}
		return nil
	}
	drain := func() error {
		if _, err := io.Copy(io.Discard, download); err != nil {
			return err
		}
		if !download.complete {
			return errors.New("archive download was not fully verified")
		}
		return nil
	}
	switch i.Download.ArchiveFormat {
	case "zip":
		tail := newZIPArchiveTail()
		if _, err = io.Copy(tail, download); err != nil {
			return nil, err
		}
		if !download.complete {
			return nil, errors.New("archive metadata was not verified")
		}
		member, inspectErr := inspectZIPArchive(tail, i.Download.ArchiveMember, s.limits.MaxBytes)
		if inspectErr != nil {
			return nil, inspectErr
		}
		if err = validateSize(member.SizeBytes); err != nil {
			return nil, err
		}
		download.Close()
		download, err = s.openVerifiedDownload(ctx, cancel, i, e)
		if err != nil {
			return nil, err
		}
		if download.size != downloadSize {
			return nil, errors.New("archive size changed between download passes")
		}
		// Preserve the completed metadata-pass progress until the upload stage.
		download.progress = nil
		selected, openErr := member.Open(download)
		if openErr != nil {
			return nil, openErr
		}
		return &archiveImageSource{reader: selected, size: member.SizeBytes, downloadSize: downloadSize, finish: drain, close: func() error { return errors.Join(selected.Close(), download.Close()) }}, nil
	case "tar", "tar.gz":
		var expanded io.Reader = download
		closeSource := download.Close
		if i.Download.ArchiveFormat == "tar.gz" {
			gz, gzipErr := gzip.NewReader(download)
			if gzipErr != nil {
				return nil, gzipErr
			}
			expanded = gz
			closeSource = func() error { return errors.Join(gz.Close(), download.Close()) }
		}
		// Include skipped members and TAR headers in the expansion budget.
		bounded := &archiveExpansionReader{reader: expanded, remaining: s.limits.MaxBytes, ctx: ctx}
		tr := tar.NewReader(bounded)
		for {
			header, nextErr := tr.Next()
			if nextErr == io.EOF {
				return nil, errors.New("archive member was not found")
			}
			if nextErr != nil {
				return nil, nextErr
			}
			if header.Name != i.Download.ArchiveMember {
				continue
			}
			if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
				return nil, errors.New("archive member must be a regular file, not a link or directory")
			}
			if header.Size < 0 {
				return nil, errors.New("negative archive member size")
			}
			size := uint64(header.Size)
			if err = validateSize(size); err != nil {
				return nil, err
			}
			finish := func() error {
				for {
					h, nextErr := tr.Next()
					if nextErr == io.EOF {
						break
					}
					if nextErr != nil {
						return nextErr
					}
					if h.Name == i.Download.ArchiveMember {
						return errors.New("duplicate archive member")
					}
				}
				// Force gzip trailer verification and hash all bytes after TAR's end marker.
				if _, err := io.Copy(tarPaddingWriter{}, bounded); err != nil {
					return err
				}
				return drain()
			}
			return &archiveImageSource{reader: tr, size: size, downloadSize: downloadSize, finish: finish, close: closeSource}, nil
		}
	default:
		return nil, errors.New("unsupported archive format")
	}
}

type archiveExpansionReader struct {
	reader    io.Reader
	remaining uint64
	ctx       context.Context
}

func (r *archiveExpansionReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.reader.Read(extra[:])
		if n > 0 {
			return 0, errors.New("expanded TAR exceeds image_transfers.max_bytes")
		}
		return 0, err
	}
	if uint64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= uint64(n)
	return n, err
}

// TAR permits zero record padding after its end marker, not a hidden second archive.
type tarPaddingWriter struct{}

func (tarPaddingWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b != 0 {
			return 0, errors.New("nonzero data after TAR end marker")
		}
	}
	return len(p), nil
}
