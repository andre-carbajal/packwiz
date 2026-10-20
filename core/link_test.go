package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestPrimaryUpdateSource(t *testing.T) {
	registerTestUpdaters(t)
	both := map[string]map[string]any{"curseforge": {}, "modrinth": {}}
	for _, tc := range []struct {
		name, explicit, mode, url, want string
		metadata                        map[string]map[string]any
	}{
		{"explicit", "modrinth", ModeCF, "", "modrinth", both},
		{"cf mode", "", ModeCF, "", "curseforge", both},
		{"mr URL", "", "", "https://cdn.modrinth.com/data/project/version/file.jar", "modrinth", both},
		{"single legacy", "", "", "https://example.org/file.jar", "modrinth", map[string]map[string]any{"modrinth": {}}},
		{"ambiguous", "", "", "https://example.org/file.jar", "", both},
		{"spoofed host", "", "", "https://cdn.modrinth.com.evil.test/file.jar", "", both},
		{"invalid explicit", "missing", "", "", "", both},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Mod{Name: tc.name, UpdateSource: tc.explicit, Update: tc.metadata, Download: ModDownload{Mode: tc.mode, URL: tc.url}}
			got, err := m.PrimaryUpdateSource()
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestLinkAndResetPreserveUserMetadata(t *testing.T) {
	registerTestUpdaters(t)
	viper.Set("cache.directory", t.TempDir())
	t.Cleanup(func() { viper.Set("cache.directory", nil) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "artifact") }))
	defer server.Close()
	h, _ := GetHashImpl("sha512")
	h.Write([]byte("artifact"))
	m := Mod{Name: "example", FileName: "example.jar", Pin: true, Side: ClientSide, UpdateSource: "modrinth", Environment: &ModEnvironment{Client: "optional", Server: "unsupported"}, Option: &ModOption{Optional: true, Default: true}, Download: ModDownload{URL: server.URL, HashFormat: "sha512", Hash: h.HashToString(h.Sum(nil))}, Update: map[string]map[string]any{"modrinth": {"mod-id": "project", "version": "version"}}}
	m.SetMetaPath(filepath.Join(t.TempDir(), "example.pw.toml"))
	before := m
	old := PlatformLinkers["curseforge"]
	t.Cleanup(func() {
		if old == nil {
			delete(PlatformLinkers, "curseforge")
		} else {
			PlatformLinkers["curseforge"] = old
		}
	})
	PlatformLinkers["curseforge"] = func(identity FileIdentity, reference string) (map[string]any, error) {
		if string(identity.Bytes) != "artifact" || identity.Hashes["sha1"] == "" {
			t.Fatal("missing identity")
		}
		return map[string]any{"project-id": uint32(1), "file-id": uint32(2)}, nil
	}
	if err := LinkMod(&m, "curseforge", ""); err != nil {
		t.Fatal(err)
	}
	if m.GetFilePath() != before.GetFilePath() || m.Download != before.Download || m.Pin != before.Pin || m.UpdateSource != before.UpdateSource || !reflect.DeepEqual(m.Option, before.Option) || !reflect.DeepEqual(m.Environment, before.Environment) {
		t.Fatal("link changed user metadata")
	}
	if _, ok := m.GetParsedUpdateData("curseforge"); !ok {
		t.Fatal("parsed link missing")
	}
	PlatformLinkers["curseforge"] = func(FileIdentity, string) (map[string]any, error) { return nil, fmt.Errorf("API unavailable") }
	ResetSecondaryReferences(&m, "modrinth")
	if _, ok := m.Update["curseforge"]; ok {
		t.Fatal("stale raw reference retained")
	}
	if _, ok := m.GetParsedUpdateData("curseforge"); ok {
		t.Fatal("stale parsed reference retained")
	}
	if _, _, err := m.Write(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadMod(m.GetFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UpdateSource != "modrinth" {
		t.Fatal("update-source did not roundtrip")
	}
}

func TestValidatePlatformReferences(t *testing.T) {
	mods := []*Mod{{Name: "B"}, {Name: "A"}}
	err := ValidatePlatformReferences(mods, "curseforge", false)
	if err == nil || !strings.Contains(err.Error(), "A, B") {
		t.Fatalf("missing actionable list: %v", err)
	}
	if err := ValidatePlatformReferences(mods, "curseforge", true); err != nil {
		t.Fatal(err)
	}
}

func TestWriteInstalledModReusesCrossPlatformPath(t *testing.T) {
	registerTestUpdaters(t)
	root := t.TempDir()
	index := Index{HashFormat: "sha256", Files: make(IndexFiles), packRoot: root, indexFile: filepath.Join(root, "index.toml")}
	old := Mod{Name: "old", FileName: "mod.jar", Pin: true, Side: ClientSide, Option: &ModOption{Optional: true}, Environment: &ModEnvironment{Client: "required", Server: "unsupported"}, Update: map[string]map[string]any{"modrinth": {"mod-id": "project"}}}
	old.SetMetaPath(filepath.Join(root, "mods", "original-slug.pw.toml"))
	format, hash, err := old.Write()
	if err != nil {
		t.Fatal(err)
	}
	if err := index.RefreshFileWithHash(old.GetFilePath(), format, hash, true); err != nil {
		t.Fatal(err)
	}
	newMod := Mod{Name: "new", FileName: "mod.jar", UpdateSource: "curseforge", Update: map[string]map[string]any{"modrinth": {"mod-id": "project"}, "curseforge": {"project-id": 1, "file-id": 2}}}
	newMod.SetMetaPath(filepath.Join(root, "mods", "different-slug.pw.toml"))
	if err := WriteInstalledMod(&newMod, &index); err != nil {
		t.Fatal(err)
	}
	if newMod.GetFilePath() != old.GetFilePath() || !newMod.Pin || !reflect.DeepEqual(newMod.Environment, old.Environment) || !reflect.DeepEqual(newMod.Option, old.Option) {
		t.Fatal("existing options/path lost")
	}
	if _, err := os.Stat(filepath.Join(root, "mods", "different-slug.pw.toml")); !os.IsNotExist(err) {
		t.Fatal("duplicate metadata created")
	}
}

func TestReadFingerprintIdentityDoesNotTrustCache(t *testing.T) {
	viper.Set("cache.directory", t.TempDir())
	t.Cleanup(func() { viper.Set("cache.directory", nil) })
	data := "abc"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, data) }))
	defer server.Close()
	hash, _ := GetHashImpl("murmur2")
	hash.Write([]byte(data))
	mod := Mod{Name: "fingerprint", FileName: "file.jar", Download: ModDownload{URL: server.URL, HashFormat: "murmur2", Hash: hash.HashToString(hash.Sum(nil))}}
	session, err := CreateDownloadSession([]*Mod{&mod}, []string{"sha512"})
	if err != nil {
		t.Fatal(err)
	}
	for dl := range session.StartDownloads() {
		if dl.Error != nil {
			t.Fatal(dl.Error)
		}
		dl.File.Close()
	}
	if err := session.SaveIndex(); err != nil {
		t.Fatal(err)
	}
	data = "a b c"
	identity, err := ReadFileIdentity(&mod)
	if err != nil {
		t.Fatal(err)
	}
	if string(identity.Bytes) != data {
		t.Fatal("fingerprint collision reused different cached bytes")
	}
}
