package modrinth

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/viper"
)

const testSHA1 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testSHA512 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func testMRPack() Pack {
	return Pack{
		FormatVersion: 1,
		Game:          "minecraft",
		VersionID:     "1.2.3",
		Name:          "Test Pack",
		Summary:       "Pack summary",
		Files: []PackFile{
			{
				Path:      "mods/modrinth.jar",
				Hashes:    map[string]string{"sha1": testSHA1, "sha512": testSHA512},
				Env:       &PackEnvironment{Client: "required", Server: "unsupported"},
				Downloads: []string{"https://cdn.modrinth.com/data/Abc123/versions/Ver456/modrinth.jar"},
			},
			{
				Path:      "mods/external.jar",
				Hashes:    map[string]string{"sha1": testSHA1, "sha512": testSHA512},
				Env:       &PackEnvironment{Client: "optional", Server: "required"},
				Downloads: []string{"https://downloads.example.com/external.jar"},
			},
			{
				Path:      "resourcepacks/pack.zip",
				Hashes:    map[string]string{"sha1": testSHA1, "sha512": testSHA512},
				Downloads: []string{"https://cdn.modrinth.com/data/Pack123/versions/Ver789/pack.zip"},
			},
		},
		Dependencies: map[string]string{
			"minecraft":     "1.21.1",
			"fabric-loader": "0.16.0",
			"future-loader": "7.0.0",
		},
	}
}

func makeMRPackArchive(t *testing.T, manifest Pack, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestFile, err := writer.Create(mrpackManifestName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestFile.Write(manifestData); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, "/") {
			if _, err := file.Write([]byte(contents)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeMRPackFile(t *testing.T, path string, archive []byte) {
	t.Helper()
	if err := os.WriteFile(path, archive, 0o644); err != nil {
		t.Fatal(err)
	}
}

func withMRPackConfig(t *testing.T, packPath string) {
	t.Helper()
	oldPackFile := viper.Get("pack-file")
	oldIndexFile := viper.Get("init.index-file")
	t.Cleanup(func() {
		viper.Set("pack-file", oldPackFile)
		viper.Set("init.index-file", oldIndexFile)
	})
	viper.Set("pack-file", packPath)
	viper.Set("init.index-file", "index.toml")
}

func TestReadMRPack(t *testing.T) {
	manifest := testMRPack()
	manifest.Files = append(manifest.Files, PackFile{
		Path:      "mods/replaced-by-override.jar",
		Hashes:    map[string]string{"sha1": testSHA1, "sha512": testSHA512},
		Downloads: []string{"https://cdn.modrinth.com/data/Over123/versions/Ver001/old.jar"},
	})
	archive := makeMRPackArchive(t, manifest, map[string]string{
		"overrides/icon.png":                      "icon",
		"client-overrides/config/client.cfg":      "client",
		"server-overrides/config/server.cfg":      "server",
		"overrides/mods/replaced-by-override.jar": "override",
	})
	got, err := readMRPack(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("readMRPack: %v", err)
	}
	if len(got.files) != 3 {
		t.Fatalf("got %d manifest files, want 3 after override precedence", len(got.files))
	}
	if got.manifest.Name != "Test Pack" {
		t.Errorf("manifest name = %q, want Test Pack", got.manifest.Name)
	}
	if got.files[0].path != "mods/modrinth.jar" || got.files[0].name != "modrinth" {
		t.Errorf("first file = %+v", got.files[0])
	}
	if got.files[0].update["modrinth"]["mod-id"] != "Abc123" || got.files[0].update["modrinth"]["version"] != "Ver456" {
		t.Errorf("Modrinth CDN update metadata = %#v", got.files[0].update)
	}
	if got.files[0].environment != (core.ModEnvironment{Client: "required", Server: "unsupported"}) {
		t.Errorf("client file environment = %+v", got.files[0].environment)
	}
	if got.files[1].update != nil {
		t.Errorf("external URL unexpectedly got Modrinth update metadata: %#v", got.files[1].update)
	}
	if !got.files[1].optional {
		t.Error("optional file was not marked optional")
	}
	if got.files[2].environment != (core.ModEnvironment{Client: "required", Server: "required"}) {
		t.Errorf("missing environment default = %+v", got.files[2].environment)
	}
	if len(got.overrides) != 4 {
		t.Fatalf("got %d override files, want 4", len(got.overrides))
	}
	paths := make(map[string]string)
	for _, override := range got.overrides {
		paths[override.path] = override.layer + ":" + override.instancePath
	}
	if paths["icon.png"] != "overrides:icon.png" {
		t.Errorf("icon target = %q", paths["icon.png"])
	}
	if paths[".packwiz-mrpack/client-overrides/config/client.cfg"] != "client-overrides:config/client.cfg" {
		t.Errorf("client target = %q", paths[".packwiz-mrpack/client-overrides/config/client.cfg"])
	}
	if paths[".packwiz-mrpack/server-overrides/config/server.cfg"] != "server-overrides:config/server.cfg" {
		t.Errorf("server target = %q", paths[".packwiz-mrpack/server-overrides/config/server.cfg"])
	}
}

func TestReadMRPackRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Pack)
	}{
		{name: "unsupported format", change: func(pack *Pack) { pack.FormatVersion = 2 }},
		{name: "wrong game", change: func(pack *Pack) { pack.Game = "other" }},
		{name: "missing sha512", change: func(pack *Pack) { delete(pack.Files[0].Hashes, "sha512") }},
		{name: "no https url", change: func(pack *Pack) { pack.Files[0].Downloads = []string{"http://example.com/file.jar"} }},
		{name: "unknown environment", change: func(pack *Pack) { pack.Files[0].Env.Client = "maybe" }},
		{name: "manifest traversal", change: func(pack *Pack) { pack.Files[0].Path = "../escape.jar" }},
		{name: "absolute path", change: func(pack *Pack) { pack.Files[0].Path = "/escape.jar" }},
		{name: "windows path", change: func(pack *Pack) { pack.Files[0].Path = `C:\escape.jar` }},
		{name: "reserved layer storage", change: func(pack *Pack) { pack.Files[0].Path = core.MRPackOverrideStorageDir + "/asset.jar" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := testMRPack()
			test.change(&manifest)
			archive := makeMRPackArchive(t, manifest, nil)
			_, err := readMRPack(bytes.NewReader(archive), int64(len(archive)))
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	for _, unsafe := range []string{"overrides/../escape", "overrides/../", "/absolute/", `overrides\escape`, "overrides/C:/escape", "overrides/.packwiz-mrpack/client-overrides/config.cfg"} {
		t.Run("archive "+unsafe, func(t *testing.T) {
			archive := makeMRPackArchive(t, testMRPack(), map[string]string{unsafe: "bad"})
			_, err := readMRPack(bytes.NewReader(archive), int64(len(archive)))
			if err == nil {
				t.Fatal("expected invalid archive path error")
			}
		})
	}
}

func TestImportMRPackRejectsMetadataOverrideBeforeCreatingPack(t *testing.T) {
	root := t.TempDir()
	packFile := filepath.Join(root, "pack.toml")
	withMRPackConfig(t, packFile)
	archivePath := filepath.Join(root, "source.mrpack")
	writeMRPackFile(t, archivePath, makeMRPackArchive(t, testMRPack(), map[string]string{"overrides/pack.toml": "not metadata"}))

	if err := importMRPack(archivePath); err == nil {
		t.Fatal("expected metadata overwrite error")
	}
	if _, err := os.Stat(packFile); !os.IsNotExist(err) {
		t.Errorf("pack.toml exists after rejected import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.toml")); !os.IsNotExist(err) {
		t.Errorf("index.toml exists after rejected import: %v", err)
	}
}

func TestImportMRPackRejectsSymlinkOverrideDestination(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	packFile := filepath.Join(root, "pack.toml")
	withMRPackConfig(t, packFile)
	archivePath := filepath.Join(root, "source.mrpack")
	writeMRPackFile(t, archivePath, makeMRPackArchive(t, testMRPack(), map[string]string{"overrides/linked/escape.txt": "bad"}))

	if err := importMRPack(archivePath); err == nil {
		t.Fatal("expected symlink path error")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Errorf("import wrote outside the pack directory: %v", err)
	}
}

func TestImportMRPackCreatesPackAndPreservesLayers(t *testing.T) {
	root := t.TempDir()
	packFile := filepath.Join(root, "pack.toml")
	withMRPackConfig(t, packFile)
	archivePath := filepath.Join(root, "source.mrpack")
	writeMRPackFile(t, archivePath, makeMRPackArchive(t, testMRPack(), map[string]string{
		"overrides/icon.png":                 "icon-bytes",
		"overrides/.packwizignore":           "!/.packwiz-mrpack/**\n",
		"client-overrides/config/client.cfg": "client-bytes",
		"server-overrides/config/server.cfg": "server-bytes",
	}))

	if err := importMRPack(archivePath); err != nil {
		t.Fatalf("importMRPack: %v", err)
	}
	pack, err := core.LoadPack()
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}
	if pack.Name != "Test Pack" || pack.Version != "1.2.3" || pack.Description != "Pack summary" {
		t.Errorf("pack metadata = %+v", pack)
	}
	if pack.Versions["fabric"] != "0.16.0" || pack.Versions["future-loader"] != "7.0.0" {
		t.Errorf("pack versions = %#v", pack.Versions)
	}
	index, err := pack.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	for _, path := range []string{
		".packwiz-mrpack/client-overrides/config/client.cfg",
		".packwiz-mrpack/server-overrides/config/server.cfg",
	} {
		if _, indexed := index.Files[path]; indexed {
			t.Errorf("internal layered override %q leaked into the installed profile index", path)
		}
	}

	modPath := filepath.Join(root, "mods", "modrinth.jar.pw.toml")
	mod, err := core.LoadMod(modPath)
	if err != nil {
		t.Fatalf("LoadMod: %v", err)
	}
	if mod.Side != core.ClientSide || mod.Environment == nil || mod.Environment.Server != "unsupported" {
		t.Errorf("imported Modrinth mod environment = %+v", mod)
	}
	update, ok := mod.GetParsedUpdateData("modrinth")
	if !ok || update.(mrUpdateData).ProjectID != "Abc123" {
		t.Errorf("imported update data = %#v, found=%v", update, ok)
	}
	optional, err := core.LoadMod(filepath.Join(root, "mods", "external.jar.pw.toml"))
	if err != nil {
		t.Fatalf("LoadMod optional: %v", err)
	}
	if optional.Option == nil || !optional.Option.Optional || !optional.Option.Default {
		t.Errorf("optional file selection = %+v, want optional and active by default", optional.Option)
	}

	for path, want := range map[string]string{
		"icon.png": "icon-bytes",
		".packwiz-mrpack/client-overrides/config/client.cfg": "client-bytes",
		".packwiz-mrpack/server-overrides/config/server.cfg": "server-bytes",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || string(data) != want {
			t.Errorf("override %s = %q, %v; want %q", path, data, err, want)
		}
	}

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	if err := cmdshared.AddModrinthOverrides(&index, writer); err != nil {
		t.Fatalf("AddModrinthOverrides: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]string)
	for _, file := range reader.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		entries[file.Name] = string(data)
	}
	wantEntries := map[string]string{
		"overrides/icon.png":                 "icon-bytes",
		"client-overrides/config/client.cfg": "client-bytes",
		"server-overrides/config/server.cfg": "server-bytes",
	}
	if len(entries) != len(wantEntries) {
		t.Fatalf("exported %d entries, want %d: %#v", len(entries), len(wantEntries), entries)
	}
	for name, want := range wantEntries {
		if entries[name] != want {
			t.Errorf("exported entry %q = %q, want %q", name, entries[name], want)
		}
	}
}

func TestImportMRPackMergesAndReplacesConflicts(t *testing.T) {
	root := t.TempDir()
	packFile := filepath.Join(root, "pack.toml")
	withMRPackConfig(t, packFile)
	if err := os.WriteFile(filepath.Join(root, "index.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "icon.png"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	pack := core.Pack{
		Name: "Existing", Version: "existing-version", Description: "Keep me", PackFormat: core.CurrentPackFormat,
		Versions: map[string]string{"minecraft": "old-mc", "forge": "old-forge"},
	}
	pack.Index.File = "index.toml"
	if err := pack.Write(); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(root, "source.mrpack")
	writeMRPackFile(t, archivePath, makeMRPackArchive(t, testMRPack(), map[string]string{"overrides/icon.png": "new"}))
	if err := importMRPack(archivePath); err != nil {
		t.Fatalf("importMRPack: %v", err)
	}
	got, err := core.LoadPack()
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Existing" || got.Version != "existing-version" || got.Description != "Keep me" {
		t.Errorf("existing pack metadata changed: %+v", got)
	}
	if got.Versions["minecraft"] != "1.21.1" || got.Versions["forge"] != "old-forge" || got.Versions["fabric"] != "0.16.0" {
		t.Errorf("merged versions = %#v", got.Versions)
	}
	icon, err := os.ReadFile(filepath.Join(root, "icon.png"))
	if err != nil || string(icon) != "new" {
		t.Errorf("icon = %q, %v; want new", icon, err)
	}
}

func TestModrinthEnvironmentFallback(t *testing.T) {
	cases := []struct {
		name string
		mod  core.Mod
		want PackEnvironment
	}{
		{name: "legacy client optional", mod: core.Mod{Side: core.ClientSide, Option: &core.ModOption{Optional: true}}, want: PackEnvironment{Client: "optional", Server: "unsupported"}},
		{name: "exact mixed environment", mod: core.Mod{Environment: &core.ModEnvironment{Client: "required", Server: "optional"}}, want: PackEnvironment{Client: "required", Server: "optional"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := getModrinthEnvironment(&test.mod); *got != test.want {
				t.Errorf("environment = %+v, want %+v", got, test.want)
			}
		})
	}
}
