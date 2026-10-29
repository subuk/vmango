package bootstrap

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestGenPasswordRejectsNonTerminal(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "password")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var output bytes.Buffer
	if err := genPassword(input, &output, io.Discard); err == nil {
		t.Fatal("expected a terminal read error")
	}
	if output.Len() != 0 {
		t.Fatal("must not emit a password hash after a terminal read error")
	}
}
