package compute

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"
)

type archiveZIPTestEntry struct {
	name   string
	data   []byte
	method uint16
	mode   os.FileMode
}

func testZIPArchive(t *testing.T, entries ...archiveZIPTestEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		member, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = member.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func testZIPTail(t *testing.T, data []byte) *zipArchiveTail {
	t.Helper()
	tail := newZIPArchiveTail()
	for start := 0; start < len(data); start += 7777 {
		part := data[start:min(start+7777, len(data))]
		if n, err := tail.Write(part); err != nil || n != len(part) {
			t.Fatal(n, err)
		}
	}
	return tail
}
func readTestZIPMember(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	member, err := inspectZIPArchive(testZIPTail(t, data), name, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	source := bytes.NewReader(data)
	r, err := member.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(out)) != member.SizeBytes {
		t.Fatal("incorrect declared size")
	}
	// The integration drains this stream to finish its independent second hash.
	if _, err = io.Copy(io.Discard, source); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestZIPArchiveDescriptorsAndForwardStreaming(t *testing.T) {
	target := make([]byte, 5<<20)
	rand.New(rand.NewSource(7)).Read(target)
	data := testZIPArchive(t,
		archiveZIPTestEntry{name: "before", data: bytes.Repeat([]byte("prefix"), 8192)},
		archiveZIPTestEntry{name: "bundle/disk.qcow2", data: target, method: zip.Deflate},
		archiveZIPTestEntry{name: "after", data: []byte("suffix")})
	if len(data) <= archiveZIPMetadataLimit {
		t.Fatal("fixture must exercise forward reads before tail")
	}
	if got := readTestZIPMember(t, data, "bundle/disk.qcow2"); !bytes.Equal(got, target) {
		t.Fatal("extracted bytes differ")
	}
}

// Force ZIP64 sizes and offsets without allocating a multi-gigabyte fixture.
func makeTestZIP64(t *testing.T, data []byte) []byte {
	t.Helper()
	central := bytes.Index(data, []byte{'P', 'K', 1, 2})
	end := bytes.LastIndex(data, []byte{'P', 'K', 5, 6})
	if central < 0 || end < 0 {
		t.Fatal("invalid ZIP fixture")
	}
	old := data[central:end]
	header := append([]byte(nil), old[:46]...)
	nameLen := int(binary.LittleEndian.Uint16(header[28:]))
	extraLen := int(binary.LittleEndian.Uint16(header[30:]))
	var extra [28]byte
	binary.LittleEndian.PutUint16(extra[:], 1)
	binary.LittleEndian.PutUint16(extra[2:], 24)
	binary.LittleEndian.PutUint64(extra[4:], uint64(binary.LittleEndian.Uint32(header[24:])))
	binary.LittleEndian.PutUint64(extra[12:], uint64(binary.LittleEndian.Uint32(header[20:])))
	binary.LittleEndian.PutUint64(extra[20:], uint64(binary.LittleEndian.Uint32(header[42:])))
	binary.LittleEndian.PutUint32(header[20:], math.MaxUint32)
	binary.LittleEndian.PutUint32(header[24:], math.MaxUint32)
	binary.LittleEndian.PutUint32(header[42:], math.MaxUint32)
	binary.LittleEndian.PutUint16(header[30:], uint16(extraLen+len(extra)))
	cd := append(header, old[46:46+nameLen+extraLen]...)
	cd = append(cd, extra[:]...)
	cd = append(cd, old[46+nameLen+extraLen:]...)
	out := append(append([]byte(nil), data[:central]...), cd...)
	var footer [56]byte
	binary.LittleEndian.PutUint32(footer[:], 0x06064b50)
	binary.LittleEndian.PutUint64(footer[4:], 44)
	binary.LittleEndian.PutUint16(footer[12:], 45)
	binary.LittleEndian.PutUint16(footer[14:], 45)
	binary.LittleEndian.PutUint64(footer[24:], 1)
	binary.LittleEndian.PutUint64(footer[32:], 1)
	binary.LittleEndian.PutUint64(footer[40:], uint64(len(cd)))
	binary.LittleEndian.PutUint64(footer[48:], uint64(central))
	var locator [20]byte
	binary.LittleEndian.PutUint32(locator[:], 0x07064b50)
	binary.LittleEndian.PutUint64(locator[8:], uint64(len(out)))
	binary.LittleEndian.PutUint32(locator[16:], 1)
	out = append(out, footer[:]...)
	out = append(out, locator[:]...)
	eocd := append([]byte(nil), data[end:]...)
	binary.LittleEndian.PutUint16(eocd[8:], math.MaxUint16)
	binary.LittleEndian.PutUint16(eocd[10:], math.MaxUint16)
	binary.LittleEndian.PutUint32(eocd[12:], math.MaxUint32)
	binary.LittleEndian.PutUint32(eocd[16:], math.MaxUint32)
	return append(out, eocd...)
}

func TestZIPArchiveZIP64(t *testing.T) {
	payload := bytes.Repeat([]byte("qcow2 bytes"), 100)
	data := makeTestZIP64(t, testZIPArchive(t, archiveZIPTestEntry{name: "disk", data: payload, method: zip.Deflate}))
	if got := readTestZIPMember(t, data, "disk"); !bytes.Equal(got, payload) {
		t.Fatal("ZIP64 member differs")
	}
}

func TestZIPArchiveSelectionAndLimits(t *testing.T) {
	good := testZIPArchive(t, archiveZIPTestEntry{name: "disk", data: []byte("disk bytes")})
	for _, name := range []string{"", "/disk", "../disk", "bundle/../disk", "bundle//disk", "bundle\\disk", "disk/", "C:disk", "missing"} {
		t.Run(name, func(t *testing.T) {
			if _, err := inspectZIPArchive(testZIPTail(t, good), name, 100); err == nil {
				t.Fatal("invalid/missing member accepted")
			}
		})
	}
	for _, entry := range []archiveZIPTestEntry{
		{name: "disk", data: []byte("target"), mode: os.ModeSymlink | 0777},
		{name: "disk", mode: os.ModeDir | 0755},
		{name: "disk", data: []byte("target"), mode: os.ModeNamedPipe | 0600},
		{name: "disk"},
	} {
		if _, err := inspectZIPArchive(testZIPTail(t, testZIPArchive(t, entry)), "disk", 100); err == nil {
			t.Fatal("non-regular or empty member accepted", entry.mode)
		}
	}
	duplicate := testZIPArchive(t, archiveZIPTestEntry{name: "disk", data: []byte("one")}, archiveZIPTestEntry{name: "disk", data: []byte("two")})
	if _, err := inspectZIPArchive(testZIPTail(t, duplicate), "disk", 100); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := inspectZIPArchive(testZIPTail(t, good), "disk", 1); err == nil {
		t.Fatal("expanded size limit ignored")
	}
	for _, mutation := range []func([]byte){
		func(p []byte) { n := bytes.Index(p, []byte{'P', 'K', 1, 2}); binary.LittleEndian.PutUint16(p[n+8:], 1) },
		func(p []byte) {
			n := bytes.Index(p, []byte{'P', 'K', 1, 2})
			binary.LittleEndian.PutUint16(p[n+10:], 99)
		},
		func(p []byte) { n := len(p) - 22; binary.LittleEndian.PutUint16(p[n+4:], 1) },
		func(p []byte) {
			n := len(p) - 22
			binary.LittleEndian.PutUint16(p[n+8:], archiveZIPRecordLimit+1)
			binary.LittleEndian.PutUint16(p[n+10:], archiveZIPRecordLimit+1)
		},
		func(p []byte) { n := len(p) - 22; binary.LittleEndian.PutUint32(p[n+12:], archiveZIPMetadataLimit+1) },
	} {
		bad := append([]byte(nil), good...)
		mutation(bad)
		if _, err := inspectZIPArchive(testZIPTail(t, bad), "disk", 100); err == nil {
			t.Fatal("invalid ZIP accepted")
		}
	}
	for _, cut := range []int{0, 1, 20, len(good) - 1} {
		if _, err := inspectZIPArchive(testZIPTail(t, good[:cut]), "disk", 100); err == nil {
			t.Fatal("truncated archive accepted")
		}
	}
}

func TestZIPArchiveCRCAndTruncatedSecondResponse(t *testing.T) {
	data := testZIPArchive(t, archiveZIPTestEntry{name: "disk", data: []byte("UNIQUE-DISK-BYTES"), method: zip.Store})
	bad := append([]byte(nil), data...)
	bad[bytes.Index(bad, []byte("UNIQUE-DISK-BYTES"))] ^= 1
	member, err := inspectZIPArchive(testZIPTail(t, bad), "disk", 100)
	if err != nil {
		t.Fatal(err)
	}
	r, err := member.Open(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(r); err == nil {
		t.Fatal("CRC mismatch accepted")
	}
	r.Close()
	large := testZIPArchive(t, archiveZIPTestEntry{name: "disk", data: bytes.Repeat([]byte("a"), archiveZIPMetadataLimit+4096), method: zip.Store})
	member, err = inspectZIPArchive(testZIPTail(t, large), "disk", 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	r, err = member.Open(bytes.NewReader(large[:35]))
	if err == nil {
		_, err = io.ReadAll(r)
		r.Close()
	}
	if err == nil {
		t.Fatal("short second response accepted")
	}
}

func TestZIPArchiveTailRingAndForwardReads(t *testing.T) {
	data := make([]byte, 2*archiveZIPMetadataLimit+123)
	rand.New(rand.NewSource(1)).Read(data)
	for _, single := range []bool{false, true} {
		tail := newZIPArchiveTail()
		if single {
			tail.Write(data)
		} else {
			tail = testZIPTail(t, data)
		}
		got := make([]byte, archiveZIPMetadataLimit)
		if n, err := tail.ReadAt(got, int64(len(data)-len(got))); err != nil || n != len(got) || !bytes.Equal(got, data[len(data)-len(got):]) {
			t.Fatal("tail ring mismatch", single, n, err)
		}
		if len(tail.data) > archiveZIPMetadataLimit {
			t.Fatal("unbounded tail")
		}
		r := &zipForwardReaderAt{tail: tail, source: bytes.NewReader(data)}
		buf := make([]byte, 10)
		if _, err := r.ReadAt(buf, 100); err != nil || !bytes.Equal(buf, data[100:110]) {
			t.Fatal("forward skip failed", err)
		}
		if _, err := r.ReadAt(buf, 99); err == nil || !strings.Contains(err.Error(), "backward") {
			t.Fatal("backward read accepted", err)
		}
		if _, err := r.ReadAt(buf, -1); err == nil {
			t.Fatal("negative offset accepted")
		}
		if _, err := r.ReadAt(buf, int64(len(data)-5)); err == nil {
			t.Fatal("past-end read accepted")
		}
	}
}

func TestZIPArchiveRejectsAmbiguousFooterBeforeStandardParser(t *testing.T) {
	good := testZIPArchive(t, archiveZIPTestEntry{name: "disk", data: []byte("disk bytes")})
	// A later footer-like sequence in a comment must not bypass the record
	// count validation by making archive/zip choose a different EOCD.
	bad := append([]byte(nil), good...)
	binary.LittleEndian.PutUint16(bad[len(bad)-2:], 23)
	var fake [23]byte
	binary.LittleEndian.PutUint32(fake[:], 0x06054b50)
	binary.LittleEndian.PutUint16(fake[8:], 60000)
	binary.LittleEndian.PutUint16(fake[10:], 60000)
	bad = append(bad, fake[:]...)
	if _, err := inspectZIPArchive(testZIPTail(t, bad), "disk", 100); err == nil {
		t.Fatal("ambiguous EOCD accepted")
	}
	// ZIP64 metadata is used by archive/zip only when classic sentinel fields
	// request it. Reject a locator whose small counts could mask classic ones.
	bad = makeTestZIP64(t, good)
	end := len(bad) - 22
	binary.LittleEndian.PutUint16(bad[end+8:], 60000)
	binary.LittleEndian.PutUint16(bad[end+10:], 60000)
	binary.LittleEndian.PutUint32(bad[end+12:], 46)
	binary.LittleEndian.PutUint32(bad[end+16:], 0)
	if _, err := inspectZIPArchive(testZIPTail(t, bad), "disk", 100); err == nil {
		t.Fatal("ambiguous ZIP64 markers accepted")
	}
	// Disallow a gap that would trigger archive/zip's alternate base offset.
	bad = append([]byte(nil), good...)
	size := binary.LittleEndian.Uint32(bad[len(bad)-10:])
	binary.LittleEndian.PutUint32(bad[len(bad)-10:], size-1)
	if _, err := inspectZIPArchive(testZIPTail(t, bad), "disk", 100); err == nil {
		t.Fatal("ambiguous central offset accepted")
	}
}
