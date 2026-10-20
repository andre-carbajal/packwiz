package modrinth

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	modrinthApi "codeberg.org/theepicblock/go-modrinth/modrinth"
	"github.com/andre-carbajal/packwiz/core"
	_ "github.com/andre-carbajal/packwiz/curseforge"
	"github.com/jarcoal/httpmock"
	"github.com/spf13/viper"
)

func mrTestIdentity(data string) core.FileIdentity {
	return core.FileIdentity{Bytes: []byte(data), Hashes: map[string]string{"sha1": fmt.Sprintf("%x", sha1.Sum([]byte(data))), "sha512": fmt.Sprintf("%x", sha512.Sum512([]byte(data)))}}
}

func mrLinkTestVersion(identity core.FileIdentity) *modrinthApi.Version {
	return &modrinthApi.Version{ID: new("Version1"), ProjectID: new("Project1"), VersionNumber: new("1.0"), Files: []*modrinthApi.File{{Filename: new("file.jar"), URL: new("https://cdn.modrinth.com/data/Project1/versions/Version1/file.jar"), Hashes: identity.Hashes, Size: new(uint32(len(identity.Bytes))), Primary: new(true)}}}
}

func TestLinkModrinthByExactHash(t *testing.T) {
	identity := mrTestIdentity("artifact")
	for _, tc := range []struct {
		name, reference, wantErrText  string
		mismatch, duplicate, apiError bool
		wantErr                       bool
	}{
		{name: "automatic"},
		{name: "different content", mismatch: true, wantErr: true, wantErrText: "identical contents"},
		{name: "automatic ambiguity", duplicate: true, wantErr: true},
		{name: "API failure", apiError: true, wantErr: true},
		{name: "manual project", reference: "https://modrinth.com/mod/example"},
		{name: "manual version", reference: "https://modrinth.com/mod/example/version/Version1"},
		{name: "ambiguous project", reference: "https://modrinth.com/mod/example", duplicate: true, wantErr: true},
		{name: "version disambiguates", reference: "https://modrinth.com/mod/example/version/Version1", duplicate: true},
		{name: "wrong URL", reference: "https://evil.test/mod/example", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpmock.Activate()
			defer httpmock.DeactivateAndReset()
			version := mrLinkTestVersion(identity)
			if tc.mismatch {
				version.Files[0].Hashes = mrTestIdentity("different").Hashes
			}
			versions := []*modrinthApi.Version{version}
			if tc.duplicate {
				other := *version
				other.ID = new("Version2")
				versions = append(versions, &other)
			}
			status := 200
			if tc.apiError {
				status = 503
			}
			httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/version_file/"+identity.Hashes["sha512"]+"?algorithm=sha512", httpmock.NewJsonResponderOrPanic(status, version))
			httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/project/example", httpmock.NewStringResponder(200, `{"id":"Project1"}`))
			httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/project/Project1/version", httpmock.NewJsonResponderOrPanic(200, versions))
			got, err := linkModrinth(identity, tc.reference)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %v, %v", got, err)
			}
			if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("error %q does not explain %q", err, tc.wantErrText)
			}
			if !tc.wantErr {
				parsed, err := (mrUpdater{}).ParseUpdate(got)
				if err != nil {
					t.Fatal(err)
				}
				if parsed.(mrUpdateData).InstalledVersion != "Version1" {
					t.Fatalf("wrong version: %+v", parsed)
				}
			}
		})
	}
}

func mrExportFixture(t *testing.T, mod *core.Mod) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	values := map[string]any{"pack-file": filepath.Join(root, "pack.toml"), "cache.directory": t.TempDir(), "modrinth.export.output": filepath.Join(t.TempDir(), "pack.mrpack"), "modrinth.export.restrictDomains": true, "modrinth.export.allow-overrides": false}
	for key, value := range values {
		old := viper.Get(key)
		viper.Set(key, value)
		t.Cleanup(func() { viper.Set(key, old) })
	}
	if err := os.WriteFile("pack.toml", []byte("name = 'Link Test'\nversion = '1.0'\npack-format = 'packwiz:1.1.0'\n[index]\nfile = 'index.toml'\nhash-format = 'sha256'\n[versions]\nminecraft = '1.20.1'\nfabric = '0.16.0'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("index.toml", []byte("hash-format = 'sha256'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("config", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/example.txt", []byte("local config"), 0644); err != nil {
		t.Fatal(err)
	}
	mod.SetMetaPath(filepath.Join(root, "mods", "example.pw.toml"))
	if _, _, err := mod.Write(); err != nil {
		t.Fatal(err)
	}
	return mod.GetFilePath(), viper.GetString("modrinth.export.output")
}

func TestModrinthExportUsesNativeLinkedFile(t *testing.T) {
	identity := mrTestIdentity("artifact")
	version := mrLinkTestVersion(identity)
	mod := core.Mod{Name: "Linked", FileName: "installed-name.jar", UpdateSource: "curseforge", Pin: true, Side: core.ClientSide, Environment: &core.ModEnvironment{Client: "optional", Server: "unsupported"}, Option: &core.ModOption{Optional: true, Default: true}, Download: core.ModDownload{Mode: core.ModeCF, HashFormat: "sha1", Hash: identity.Hashes["sha1"]}, Update: map[string]map[string]any{"curseforge": {"project-id": uint32(1), "file-id": uint32(10)}, "modrinth": {"mod-id": "Project1", "version": "Version1"}}}
	metaPath, output := mrExportFixture(t, &mod)
	before, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/version/Version1", httpmock.NewJsonResponderOrPanic(200, version))
	httpmock.RegisterResponder("GET", *version.Files[0].URL, httpmock.NewStringResponder(200, "artifact"))
	if err := exportCmd.RunE(exportCmd, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("export rewrote installed metadata")
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var manifest Pack
	foundConfig := false
	for _, file := range zr.File {
		if strings.HasSuffix(file.Name, ".jar") {
			t.Fatalf("linked jar in overrides: %s", file.Name)
		}
		if file.Name == "overrides/config/example.txt" {
			foundConfig = true
		}
		if file.Name == "modrinth.index.json" {
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
	if !foundConfig || len(manifest.Files) != 1 {
		t.Fatalf("missing manifest/config: %+v", manifest)
	}
	file := manifest.Files[0]
	if file.Path != "mods/installed-name.jar" || file.Downloads[0] != *version.Files[0].URL || file.Hashes["sha512"] != identity.Hashes["sha512"] || file.FileSize != uint32(len(identity.Bytes)) || file.Env.Client != "optional" || file.Env.Server != "unsupported" {
		t.Fatalf("wrong native file: %+v", file)
	}
}

func TestModrinthStrictExportPreservesExistingOutput(t *testing.T) {
	identity := mrTestIdentity("artifact")
	mod := core.Mod{Name: "External", FileName: "external.jar", Download: core.ModDownload{URL: "https://outside.test/file.jar", HashFormat: "sha512", Hash: identity.Hashes["sha512"]}}
	_, output := mrExportFixture(t, &mod)
	if err := os.WriteFile(output, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	err := exportCmd.RunE(exportCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "External") {
		t.Fatalf("strict error: %v", err)
	}
	kept, _ := os.ReadFile(output)
	if string(kept) != "keep me" {
		t.Fatal("output was overwritten")
	}
	viper.Set("modrinth.export.allow-overrides", true)
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("GET", mod.Download.URL, httpmock.NewStringResponder(503, "failure"))
	if err := exportCmd.RunE(exportCmd, nil); err == nil {
		t.Fatal("download should fail")
	}
	kept, _ = os.ReadFile(output)
	if string(kept) != "keep me" {
		t.Fatal("download failure overwrote output")
	}
	httpmock.RegisterResponder("GET", mod.Download.URL, httpmock.NewStringResponder(200, "artifact"))
	if err := exportCmd.RunE(exportCmd, nil); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, file := range zr.File {
		if file.Name == "overrides/mods/external.jar" {
			r, _ := file.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			if string(data) != "artifact" {
				t.Fatal("bad override bytes")
			}
			return
		}
	}
	t.Fatal("explicit override was not bundled")
}

func TestPlatformExportModRejectsStaleReference(t *testing.T) {
	identity := mrTestIdentity("artifact")
	version := mrLinkTestVersion(mrTestIdentity("different"))
	mod := core.Mod{Name: "Stale", Download: core.ModDownload{HashFormat: "sha512", Hash: identity.Hashes["sha512"]}, Update: map[string]map[string]any{"modrinth": {"mod-id": "Project1", "version": "Version1"}}}
	if err := mod.ParseUpdateData(); err != nil {
		t.Fatal(err)
	}
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/version/Version1", httpmock.NewJsonResponderOrPanic(200, version))
	if _, err := platformExportMod(&mod); err == nil {
		t.Fatal("stale reference exported")
	}
}

func TestModrinthUpdateReplacesSecondaryReference(t *testing.T) {
	identity := mrTestIdentity("updated artifact")
	version := mrLinkTestVersion(identity)
	version.ID = new("Version2")
	mod := core.Mod{Name: "Update", FileName: "old.jar", UpdateSource: "modrinth", Pin: false, Environment: &core.ModEnvironment{Client: "optional", Server: "unsupported"}, Download: core.ModDownload{URL: "https://cdn.modrinth.com/old.jar", HashFormat: "sha512", Hash: mrTestIdentity("old artifact").Hashes["sha512"]}, Update: map[string]map[string]any{"modrinth": {"mod-id": "Project1", "version": "Version1"}, "curseforge": {"project-id": uint32(1), "file-id": uint32(10)}}}
	oldCache := viper.Get("cache.directory")
	viper.Set("cache.directory", t.TempDir())
	t.Cleanup(func() { viper.Set("cache.directory", oldCache) })
	t.Setenv("CURSEFORGE_API_KEY", "test-key")
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("GET", *version.Files[0].URL, httpmock.NewStringResponder(200, string(identity.Bytes)))
	httpmock.RegisterResponder("POST", "https://api.curseforge.com/v1/fingerprints", httpmock.NewStringResponder(200, fmt.Sprintf(`{"data":{"exactMatches":[{"id":1,"file":{"id":20,"modId":1,"hashes":[{"algo":1,"value":%q}]}}]}}`, identity.Hashes["sha1"])))
	if err := (mrUpdater{}).DoUpdate([]*core.Mod{&mod}, []any{cachedStateStore{ProjectID: "Project1", Version: version}}); err != nil {
		t.Fatal(err)
	}
	if mod.Update["curseforge"]["file-id"] != uint32(20) || mod.UpdateSource != "modrinth" || mod.Download.Hash != identity.Hashes["sha512"] || mod.Environment.Client != "optional" {
		t.Fatalf("update left stale reference/settings: %+v", mod)
	}
	parsed, ok := mod.GetParsedUpdateData("curseforge")
	if !ok || parsed == nil {
		t.Fatal("new reference not parsed")
	}
}

func TestAddingModrinthFileAutomaticallyLinksOrWarns(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			identity := mrTestIdentity("artifact")
			version := mrLinkTestVersion(identity)
			version.Environment = new("client_and_server")
			version.Loaders = []string{"fabric"}
			project := &modrinthApi.Project{ID: new("Project1"), Slug: new("example"), Title: new("Example"), ProjectType: new("mod")}
			_, _ = mrExportFixture(t, &core.Mod{Name: "Unrelated", FileName: "unrelated.jar"})
			root, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]any{"meta-folder-base": root, "meta-folder": "mods"} {
				old := viper.Get(key)
				viper.Set(key, value)
				t.Cleanup(func() { viper.Set(key, old) })
			}
			index, err := core.LoadIndex(filepath.Join(root, "index.toml"))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CURSEFORGE_API_KEY", "test-key")
			httpmock.Activate()
			defer httpmock.DeactivateAndReset()
			httpmock.RegisterResponder("GET", *version.Files[0].URL, httpmock.NewStringResponder(200, "artifact"))
			status := 200
			if fail {
				status = 503
			}
			httpmock.RegisterResponder("POST", "https://api.curseforge.com/v1/fingerprints", httpmock.NewStringResponder(status, fmt.Sprintf(`{"data":{"exactMatches":[{"id":1,"file":{"id":10,"modId":1,"hashes":[{"algo":1,"value":%q}]}}]}}`, identity.Hashes["sha1"])))
			if err := createFileMeta(project, version, version.Files[0], core.Pack{Versions: map[string]string{"minecraft": "1.20.1", "fabric": "0.16.0"}}, &index); err != nil {
				t.Fatal(err)
			}
			installed, err := core.LoadMod(filepath.Join(root, "mods", "example.pw.toml"))
			if err != nil {
				t.Fatal(err)
			}
			_, linked := installed.Update["curseforge"]
			if linked == fail || installed.Update["modrinth"] == nil || installed.UpdateSource != "modrinth" || installed.Download.URL != *version.Files[0].URL {
				t.Fatalf("primary/link not preserved: %+v", installed)
			}
		})
	}
}
