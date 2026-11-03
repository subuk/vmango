package compute

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
)

const (
	archiveZIPMetadataLimit = 4 << 20
	archiveZIPRecordLimit   = 10000
)

// zipArchiveTail retains a bounded metadata window while the caller verifies
// the complete archive. Payload bytes are never written on the application host.
type zipArchiveTail struct {
	data  []byte
	next  int
	total int64
}

func newZIPArchiveTail() *zipArchiveTail { return &zipArchiveTail{} }

func (t *zipArchiveTail) Write(p []byte) (int, error) {
	n := len(p)
	if int64(n) > math.MaxInt64-t.total {
		return 0, errors.New("ZIP archive size overflows")
	}
	t.total += int64(n)
	if len(p) >= archiveZIPMetadataLimit {
		t.data = append(t.data[:0], p[len(p)-archiveZIPMetadataLimit:]...)
		t.next = 0
		return n, nil
	}
	if len(t.data) < archiveZIPMetadataLimit {
		copied := min(len(p), archiveZIPMetadataLimit-len(t.data))
		t.data = append(t.data, p[:copied]...)
		p = p[copied:]
		if len(p) == 0 {
			return n, nil
		}
	}
	copied := copy(t.data[t.next:], p)
	t.next = (t.next + copied) % archiveZIPMetadataLimit
	if copied < len(p) {
		t.next = copy(t.data, p[copied:])
	}
	return n, nil
}

func (t *zipArchiveTail) ReadAt(p []byte, off int64) (int, error) {
	start := t.total - int64(len(t.data))
	if off < start || off < 0 || off > t.total {
		return 0, errors.New("ZIP directory exceeds the 4 MiB metadata limit")
	}
	if len(p) == 0 {
		return 0, nil
	}
	available := min(int64(len(p)), t.total-off)
	index := int(off - start)
	if len(t.data) == archiveZIPMetadataLimit {
		index = (index + t.next) % archiveZIPMetadataLimit
	}
	n := copy(p[:available], t.data[index:])
	if int64(n) < available {
		n += copy(p[n:available], t.data[:int(available)-n])
	}
	if n != len(p) {
		return n, io.EOF
	}
	return n, nil
}

func validArchiveMemberName(name string) bool {
	return name != "" && name != "." && name != ".." && !path.IsAbs(name) &&
		path.Clean(name) == name && !strings.HasPrefix(name, "../") &&
		!strings.ContainsAny(name, "\\\x00:")
}

// validateZIPDirectory bounds archive/zip allocations before parsing untrusted
// metadata. Offsets are absolute: split archives and self-extracting stubs with
// relative offsets are not supported.
func validateZIPDirectory(t *zipArchiveTail) error {
	window := make([]byte, min(t.total, 22+65535))
	if _, err := t.ReadAt(window, t.total-int64(len(window))); err != nil {
		return err
	}
	end := -1
	for n := len(window) - 22; n >= 0; n-- {
		if binary.LittleEndian.Uint32(window[n:]) == 0x06054b50 {
			// archive/zip also chooses the last signature, including signatures
			// embedded in comments. Never validate one footer and parse another.
			if n+22+int(binary.LittleEndian.Uint16(window[n+20:])) != len(window) {
				return errors.New("ambiguous or truncated ZIP end record")
			}
			end = n
			break
		}
	}
	if end < 0 {
		return errors.New("invalid or truncated ZIP directory")
	}
	e := window[end:]
	if binary.LittleEndian.Uint16(e[4:]) != 0 || binary.LittleEndian.Uint16(e[6:]) != 0 {
		return errors.New("split ZIP archives are unsupported")
	}
	records := uint64(binary.LittleEndian.Uint16(e[10:]))
	if uint64(binary.LittleEndian.Uint16(e[8:])) != records {
		return errors.New("inconsistent ZIP directory record counts")
	}
	size := uint64(binary.LittleEndian.Uint32(e[12:]))
	offset := uint64(binary.LittleEndian.Uint32(e[16:]))
	footer := t.total - int64(len(window)) + int64(end)
	var locator [20]byte
	hasLocator := footer >= 20 && func() bool {
		_, err := t.ReadAt(locator[:], footer-20)
		return err == nil && binary.LittleEndian.Uint32(locator[:]) == 0x07064b50
	}()
	zip64 := records == math.MaxUint16 || size == math.MaxUint32 || offset == math.MaxUint32
	if hasLocator && !zip64 {
		return errors.New("ZIP64 locator requires ZIP64 end record markers")
	}
	if zip64 {
		if !hasLocator || binary.LittleEndian.Uint32(locator[4:]) != 0 || binary.LittleEndian.Uint32(locator[16:]) != 1 {
			return errors.New("invalid or split ZIP64 directory")
		}
		position := binary.LittleEndian.Uint64(locator[8:])
		if position > uint64(footer-20) || uint64(footer-20)-position < 56 {
			return errors.New("invalid ZIP64 directory offset")
		}
		var z [56]byte
		if _, err := t.ReadAt(z[:], int64(position)); err != nil {
			return err
		}
		length := binary.LittleEndian.Uint64(z[4:])
		if binary.LittleEndian.Uint32(z[:]) != 0x06064b50 || length < 44 || length != uint64(footer-20)-position-12 {
			return errors.New("invalid ZIP64 directory record")
		}
		if binary.LittleEndian.Uint32(z[16:]) != 0 || binary.LittleEndian.Uint32(z[20:]) != 0 || binary.LittleEndian.Uint64(z[24:]) != binary.LittleEndian.Uint64(z[32:]) {
			return errors.New("split ZIP64 archives are unsupported")
		}
		records = binary.LittleEndian.Uint64(z[32:])
		size = binary.LittleEndian.Uint64(z[40:])
		offset = binary.LittleEndian.Uint64(z[48:])
		footer = int64(position)
	}
	if records > archiveZIPRecordLimit || size > archiveZIPMetadataLimit || records > size/46 {
		return errors.New("ZIP directory exceeds metadata or record limits")
	}
	if offset > uint64(footer) || size != uint64(footer)-offset || offset < uint64(t.total-int64(len(t.data))) {
		return errors.New("ZIP directory lies outside the bounded metadata window")
	}
	return nil
}

type zipArchiveMember struct {
	SizeBytes uint64
	file      *zip.File
	reader    *zipForwardReaderAt
	opened    bool
}

func inspectZIPArchive(t *zipArchiveTail, name string, maxBytes uint64) (*zipArchiveMember, error) {
	if !validArchiveMemberName(name) {
		return nil, errors.New("archive member must be an exact relative path")
	}
	if err := validateZIPDirectory(t); err != nil {
		return nil, err
	}
	r := &zipForwardReaderAt{tail: t}
	archive, err := zip.NewReader(r, t.total)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("read ZIP directory: %w", err)
	}
	if len(archive.File) > archiveZIPRecordLimit {
		return nil, errors.New("ZIP directory exceeds the record limit")
	}
	var selected *zip.File
	for _, f := range archive.File {
		if f.Name != name {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("archive contains duplicate member %q", name)
		}
		selected = f
	}
	if selected == nil {
		return nil, fmt.Errorf("archive member %q was not found", name)
	}
	if !selected.Mode().IsRegular() || selected.Flags&0x41 != 0 || (selected.Method != zip.Store && selected.Method != zip.Deflate) {
		return nil, errors.New("ZIP member must be a regular, unencrypted Store or Deflate file")
	}
	if selected.UncompressedSize64 == 0 || selected.UncompressedSize64 > maxBytes || selected.CompressedSize64 > uint64(t.total) {
		return nil, errors.New("ZIP member exceeds extraction limits or has invalid size")
	}
	return &zipArchiveMember{SizeBytes: selected.UncompressedSize64, file: selected, reader: r}, nil
}

// Open uses a second verified sequential response. The caller must drain and
// verify that entire response before publishing the extracted image as ready.
func (m *zipArchiveMember) Open(source io.Reader) (io.ReadCloser, error) {
	if m.opened || source == nil {
		return nil, errors.New("ZIP member needs one fresh source stream")
	}
	m.opened = true
	m.reader.source = source
	return m.file.Open()
}

// zipForwardReaderAt serves the cached directory and forward reads of one member.
// Skipped bytes still pass through the caller's archive checksum reader.
type zipForwardReaderAt struct {
	tail   *zipArchiveTail
	source io.Reader
	offset int64
}

func (r *zipForwardReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > r.tail.total || int64(len(p)) > r.tail.total-off {
		return 0, io.ErrUnexpectedEOF
	}
	if off >= r.tail.total-int64(len(r.tail.data)) {
		return r.tail.ReadAt(p, off)
	}
	if r.source == nil {
		return 0, errors.New("ZIP directory lies outside the bounded metadata window")
	}
	if off < r.offset {
		return 0, errors.New("ZIP member requires a backward source read")
	}
	if off > r.offset {
		n, err := io.CopyN(io.Discard, r.source, off-r.offset)
		r.offset += n
		if err != nil {
			return 0, err
		}
	}
	n, err := io.ReadFull(r.source, p)
	r.offset += int64(n)
	return n, err
}
