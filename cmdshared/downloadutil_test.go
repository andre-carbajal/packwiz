package cmdshared

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/andre-carbajal/packwiz/core"
)

func TestAddNonMetafileOverridesExportsAllAliases(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "mods", "dual.jar")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	const contents = "same file, two exported paths"
	if err := os.WriteFile(source, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := filepath.Join(root, "index.toml")
	indexData := `hash-format = "sha256"

[[files]]
file = "mods/dual.jar"

[[files]]
file = "mods/dual.jar"
alias = "alt/dual.jar"
`
	if err := os.WriteFile(indexPath, []byte(indexData), 0o644); err != nil {
		t.Fatal(err)
	}
	index, err := core.LoadIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	AddNonMetafileOverrides(&index, writer)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		got[file.Name] = string(data)
	}

	want := map[string]string{
		"overrides/mods/dual.jar": contents,
		"overrides/alt/dual.jar":  contents,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d archive files, want %d: %v", len(got), len(want), got)
	}
	for name, contents := range want {
		if got[name] != contents {
			t.Errorf("archive entry %q = %q, want %q", name, got[name], contents)
		}
	}
}
