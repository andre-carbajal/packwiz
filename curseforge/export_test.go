package curseforge

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andre-carbajal/packwiz/core"
	_ "github.com/andre-carbajal/packwiz/modrinth"
	"github.com/jarcoal/httpmock"
	"github.com/spf13/viper"
)

func cfExportFixture(t *testing.T, linked bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	values := map[string]any{"pack-file": filepath.Join(root, "pack.toml"), "cache.directory": t.TempDir(), "curseforge.export.output": filepath.Join(t.TempDir(), "pack.zip"), "curseforge.export.side": "both", "curseforge.export.allow-overrides": false}
	for key, value := range values {
		old := viper.Get(key)
		viper.Set(key, value)
		t.Cleanup(func() { viper.Set(key, old) })
	}
	for path, data := range map[string]string{"pack.toml": "name = 'Link Test'\nversion = '1.0'\npack-format = 'packwiz:1.1.0'\n[index]\nfile = 'index.toml'\nhash-format = 'sha256'\n[versions]\nminecraft = '1.20.1'\nfabric = '0.16.0'\n", "index.toml": "hash-format = 'sha256'\n"} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir("config", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/example.txt", []byte("local config"), 0644); err != nil {
		t.Fatal(err)
	}
	mod := core.Mod{Name: "Example", FileName: "file.jar", UpdateSource: "modrinth", Download: core.ModDownload{URL: "https://cdn.modrinth.com/data/Project1/versions/Version1/file.jar", HashFormat: "sha512", Hash: cfTestIdentity("artifact").Hashes["sha512"]}, Update: map[string]map[string]any{"modrinth": {"mod-id": "Project1", "version": "Version1"}}}
	if linked {
		mod.Update["curseforge"] = map[string]any{"project-id": uint32(1), "file-id": uint32(10)}
	}
	mod.SetMetaPath(filepath.Join(root, "mods", "example.pw.toml"))
	if _, _, err := mod.Write(); err != nil {
		t.Fatal(err)
	}
	return mod.GetFilePath(), viper.GetString("curseforge.export.output")
}

func TestCurseForgeExportUsesNativeReference(t *testing.T) {
	metaPath, output := cfExportFixture(t, true)
	before, _ := os.ReadFile(metaPath)
	if err := exportCmd.RunE(exportCmd, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(metaPath)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("export changed primary metadata")
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var manifest struct {
		Files []struct{ ProjectID, FileID uint32 }
	}
	foundConfig := false
	for _, file := range zr.File {
		if strings.HasSuffix(file.Name, ".jar") {
			t.Fatalf("linked JAR in overrides: %s", file.Name)
		}
		if file.Name == "overrides/config/example.txt" {
			foundConfig = true
		}
		if file.Name == "manifest.json" {
			r, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			err = json.NewDecoder(r).Decode(&manifest)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if !foundConfig || len(manifest.Files) != 1 || manifest.Files[0].ProjectID != 1 || manifest.Files[0].FileID != 10 {
		t.Fatalf("wrong manifest/config: %+v", manifest)
	}
}

func TestCurseForgeExportStrictAndExplicitOverrides(t *testing.T) {
	_, output := cfExportFixture(t, false)
	if err := os.WriteFile(output, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	err := exportCmd.RunE(exportCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "Example") {
		t.Fatalf("missing strict error: %v", err)
	}
	kept, _ := os.ReadFile(output)
	if string(kept) != "keep me" {
		t.Fatal("strict failure overwrote output")
	}
	viper.Set("curseforge.export.allow-overrides", true)
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("GET", "https://cdn.modrinth.com/data/Project1/versions/Version1/file.jar", httpmock.NewStringResponder(503, "failed"))
	if err := exportCmd.RunE(exportCmd, nil); err == nil {
		t.Fatal("download should fail")
	}
	kept, _ = os.ReadFile(output)
	if string(kept) != "keep me" {
		t.Fatal("download failure overwrote output")
	}
	httpmock.RegisterResponder("GET", "https://cdn.modrinth.com/data/Project1/versions/Version1/file.jar", httpmock.NewStringResponder(200, "artifact"))
	if err := exportCmd.RunE(exportCmd, nil); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, file := range zr.File {
		if file.Name == "overrides/mods/file.jar" {
			return
		}
	}
	t.Fatal("explicit override not bundled")
}
