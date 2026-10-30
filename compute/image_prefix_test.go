package compute

import (
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestBoundedImagePrefixValidation(t *testing.T) {
	header := make([]byte, 104)
	copy(header, []byte{'Q', 'F', 'I', 0xfb})
	binary.BigEndian.PutUint32(header[4:], 3)
	binary.BigEndian.PutUint64(header[24:], 8<<30)
	capacity, err := inspectImagePrefix(header, VolumeFormatQcow2, 4096)
	if err != nil || capacity != 8<<30 {
		t.Fatal(capacity, err)
	}
	for _, offset := range []int{8, 16, 32, 72} {
		copyHeader := append([]byte(nil), header...)
		copyHeader[offset] = 1
		if _, err := inspectImagePrefix(copyHeader, VolumeFormatQcow2, 4096); err == nil {
			t.Fatalf("unsafe header field at %d accepted", offset)
		}
	}
	if _, err := inspectImagePrefix(header, VolumeFormatRaw, 4096); err == nil {
		t.Fatal("QCOW2 accepted as raw")
	}
	if _, err := inspectImagePrefix([]byte{0x1f, 0x8b}, VolumeFormatRaw, 4096); err == nil {
		t.Fatal("archive accepted as raw")
	}
}

func TestImageReaderEmptyBufferDoesNotConsumeSource(t *testing.T) {
	source := strings.NewReader("image")
	reader := &verifiedImageReader{reader: source, size: 5}
	if n, err := reader.Read(nil); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	remaining, err := io.ReadAll(source)
	if err != nil || string(remaining) != "image" {
		t.Fatal("empty read consumed image bytes", string(remaining), err)
	}
}
