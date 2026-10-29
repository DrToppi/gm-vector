package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestRecoveryTransportCapturesAndRemovesTemporaryFile(t *testing.T) {
	for _, fail := range []bool{false, true} {
		p := filepath.Join(t.TempDir(), "recovery.txt")
		link := []byte("https://synthetic.invalid/#setup=" + string(bytes.Repeat([]byte{'a'}, 43)))
		if err := os.WriteFile(p, link, 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		var writer io.Writer = &out
		if fail {
			writer = failedOutput{}
		}
		err := transferRecoveryFile(p, writer)
		if fail {
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal("output failure lost", err)
			}
		} else if err != nil || !bytes.Equal(out.Bytes(), link) {
			t.Fatal("captured transport failed", err)
		}
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("temporary secret left behind")
		}
	}
}
