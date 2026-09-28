package core

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFilePreservesOriginalOnWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pack.toml")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated encode failure")
	err := atomicWriteFile(path, func(w io.Writer) error {
		if _, err := io.WriteString(w, "partial replacement"); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("atomicWriteFile error = %v, want %v", err, wantErr)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("file after failed write = %q, want original contents", got)
	}
}
