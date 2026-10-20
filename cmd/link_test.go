package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/viper"
)

func TestLinkAllStopsAfterAuthenticationFailure(t *testing.T) {
	path, _, _ := updateLinkFixture(t, false)
	oldCache := viper.Get("cache.directory")
	viper.Set("cache.directory", t.TempDir())
	t.Cleanup(func() { viper.Set("cache.directory", oldCache) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "artifact") }))
	defer server.Close()
	first, err := core.LoadMod(path)
	if err != nil {
		t.Fatal(err)
	}
	sha512, _ := core.GetHashImpl("sha512")
	sha512.Write([]byte("artifact"))
	first.Download = core.ModDownload{URL: server.URL, HashFormat: "sha512", Hash: sha512.HashToString(sha512.Sum(nil))}
	delete(first.Update, "curseforge")
	firstFormat, firstHash, err := first.Write()
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Name = "second"
	second.Update = map[string]map[string]any{"modrinth": {"mod-id": "project-2", "version": "version-2"}}
	second.SetMetaPath(filepath.Join(filepath.Dir(path), "second.pw.toml"))
	format, hash, err := second.Write()
	if err != nil {
		t.Fatal(err)
	}
	index, err := core.LoadIndex(filepath.Join(filepath.Dir(filepath.Dir(path)), "index.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := index.RefreshFileWithHash(path, firstFormat, firstHash, true); err != nil {
		t.Fatal(err)
	}
	if err := index.RefreshFileWithHash(second.GetFilePath(), format, hash, true); err != nil {
		t.Fatal(err)
	}
	if err := index.Write(); err != nil {
		t.Fatal(err)
	}
	oldLinker := core.PlatformLinkers["curseforge"]
	calls := 0
	core.PlatformLinkers["curseforge"] = func(core.FileIdentity, string) (map[string]any, error) {
		calls++
		return nil, &core.PlatformAuthError{Platform: "CurseForge"}
	}
	t.Cleanup(func() {
		if oldLinker == nil {
			delete(core.PlatformLinkers, "curseforge")
		} else {
			core.PlatformLinkers["curseforge"] = oldLinker
		}
	})
	linkCmd.Flags().Set("all", "true")
	linkCmd.Flags().Set("curseforge", "")
	linkCmd.Flags().Set("modrinth", "")
	t.Cleanup(func() {
		linkCmd.Flags().Set("all", "false")
		linkCmd.Flags().Set("curseforge", "")
		linkCmd.Flags().Set("modrinth", "")
	})
	err = linkCmd.RunE(linkCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "CURSEFORGE_API_KEY") {
		t.Fatalf("missing key guidance: %v", err)
	}
	if !strings.Contains(err.Error(), "Link summary: 0 linked successfully, 0 already linked, 1 failed, 1 skipped.") || !strings.Contains(err.Error(), "Not attempted after CurseForge authentication failed: second") {
		t.Fatalf("missing partial-result summary: %v", err)
	}
	if !linkCmd.SilenceUsage {
		t.Fatal("link errors should not print the usage text")
	}
	if calls != 1 {
		t.Fatalf("made %d unauthorized calls, want 1", calls)
	}
}

type countingLinkUpdater struct {
	checks, updates int
	source          string
}

func (u *countingLinkUpdater) ParseUpdate(data map[string]any) (any, error) { return data, nil }
func (u *countingLinkUpdater) CheckUpdate(mods []*core.Mod, pack core.Pack) ([]core.UpdateCheck, error) {
	u.checks += len(mods)
	result := make([]core.UpdateCheck, len(mods))
	for i := range result {
		result[i] = core.UpdateCheck{UpdateAvailable: true, UpdateString: "new"}
	}
	return result, nil
}
func (u *countingLinkUpdater) DoUpdate(mods []*core.Mod, state []any) error {
	u.updates += len(mods)
	for _, mod := range mods {
		core.ResetSecondaryReferences(mod, u.source)
	}
	return nil
}

func updateLinkFixture(t *testing.T, pin bool) (string, *countingLinkUpdater, *countingLinkUpdater) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	cf, mr := &countingLinkUpdater{source: "curseforge"}, &countingLinkUpdater{source: "modrinth"}
	for source, updater := range map[string]core.Updater{"curseforge": cf, "modrinth": mr} {
		old := core.Updaters[source]
		core.Updaters[source] = updater
		t.Cleanup(func() {
			if old == nil {
				delete(core.Updaters, source)
			} else {
				core.Updaters[source] = old
			}
		})
	}
	values := map[string]any{"pack-file": filepath.Join(root, "pack.toml"), "non-interactive": true, "update.all": false}
	for key, value := range values {
		old := viper.Get(key)
		viper.Set(key, value)
		t.Cleanup(func() { viper.Set(key, old) })
	}
	if err := os.WriteFile("pack.toml", []byte("name = 'test'\npack-format = 'packwiz:1.1.0'\n[index]\nfile = 'index.toml'\nhash-format = 'sha256'\n[versions]\nminecraft = '1.20.1'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("index.toml", []byte("hash-format = 'sha256'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mod := core.Mod{Name: "example", FileName: "example.jar", Pin: pin, UpdateSource: "modrinth", Download: core.ModDownload{URL: "https://cdn.modrinth.com/data/project/version/file.jar"}, Update: map[string]map[string]any{"curseforge": {"project-id": 1, "file-id": 10}, "modrinth": {"mod-id": "project", "version": "version"}}}
	mod.SetMetaPath(filepath.Join(root, "mods", "example.pw.toml"))
	format, hash, err := mod.Write()
	if err != nil {
		t.Fatal(err)
	}
	index, err := core.LoadIndex(filepath.Join(root, "index.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := index.RefreshFileWithHash(mod.GetFilePath(), format, hash, true); err != nil {
		t.Fatal(err)
	}
	if err := index.Write(); err != nil {
		t.Fatal(err)
	}
	return mod.GetFilePath(), cf, mr
}

func TestUpdateUsesOnlyPrimarySource(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, pin := range []bool{false, true} {
			t.Run(fmt.Sprintf("all=%v,pin=%v", all, pin), func(t *testing.T) {
				path, cf, mr := updateLinkFixture(t, pin)
				viper.Set("update.all", all)
				args := []string{"example"}
				if all {
					args = nil
				}
				err := UpdateCmd.RunE(UpdateCmd, args)
				if pin && !all {
					if err == nil || !strings.Contains(err.Error(), "pinned") {
						t.Fatalf("pinned error: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if cf.checks != 0 || cf.updates != 0 {
					t.Fatal("secondary updater invoked")
				}
				want := 1
				if pin {
					want = 0
				}
				if mr.updates != want {
					t.Fatalf("updated %d times, want %d", mr.updates, want)
				}
				mod, err := core.LoadMod(path)
				if err != nil {
					t.Fatal(err)
				}
				_, secondary := mod.Update["curseforge"]
				if secondary != pin {
					t.Fatal("stale reference not invalidated or pinned mod modified")
				}
			})
		}
	}
}

func TestLinkRejectsInvalidArgumentCombinations(t *testing.T) {
	for _, tc := range []struct {
		all    bool
		cf, mr string
		args   []string
	}{
		{}, {all: true, args: []string{"example"}}, {all: true, cf: "https://example.com"}, {cf: "x", mr: "y", args: []string{"example"}},
	} {
		linkCmd.Flags().Set("all", fmt.Sprint(tc.all))
		linkCmd.Flags().Set("curseforge", tc.cf)
		linkCmd.Flags().Set("modrinth", tc.mr)
		if err := linkCmd.RunE(linkCmd, tc.args); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	t.Cleanup(func() {
		linkCmd.Flags().Set("all", "false")
		linkCmd.Flags().Set("curseforge", "")
		linkCmd.Flags().Set("modrinth", "")
	})
}

func TestLinkCommandCompletesExistingModsWithoutChangingPrimary(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			path, _, _ := updateLinkFixture(t, true)
			oldCache := viper.Get("cache.directory")
			viper.Set("cache.directory", t.TempDir())
			t.Cleanup(func() { viper.Set("cache.directory", oldCache) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "artifact") }))
			defer server.Close()
			mod, err := core.LoadMod(path)
			if err != nil {
				t.Fatal(err)
			}
			delete(mod.Update, "curseforge")
			hash, _ := core.GetHashImpl("sha512")
			hash.Write([]byte("artifact"))
			mod.Download = core.ModDownload{URL: server.URL, HashFormat: "sha512", Hash: hash.HashToString(hash.Sum(nil))}
			mod.Option = &core.ModOption{Optional: true, Default: true}
			mod.Environment = &core.ModEnvironment{Client: "optional", Server: "unsupported"}
			if _, _, err := mod.Write(); err != nil {
				t.Fatal(err)
			}
			old := core.PlatformLinkers["curseforge"]
			t.Cleanup(func() {
				if old == nil {
					delete(core.PlatformLinkers, "curseforge")
				} else {
					core.PlatformLinkers["curseforge"] = old
				}
			})
			calls := 0
			core.PlatformLinkers["curseforge"] = func(identity core.FileIdentity, reference string) (map[string]any, error) {
				calls++
				if string(identity.Bytes) != "artifact" {
					t.Fatal("wrong identity")
				}
				if !all && reference != "https://www.curseforge.com/minecraft/mc-mods/example" {
					t.Fatal("manual URL not forwarded")
				}
				return map[string]any{"project-id": 1, "file-id": 10}, nil
			}
			linkCmd.Flags().Set("all", fmt.Sprint(all))
			linkCmd.Flags().Set("modrinth", "")
			reference := "https://www.curseforge.com/minecraft/mc-mods/example"
			if all {
				reference = ""
			}
			linkCmd.Flags().Set("curseforge", reference)
			t.Cleanup(func() { linkCmd.Flags().Set("all", "false"); linkCmd.Flags().Set("curseforge", "") })
			args := []string{"example"}
			if all {
				args = nil
			}
			if err := linkCmd.RunE(linkCmd, args); err != nil {
				t.Fatal(err)
			}
			linked, err := core.LoadMod(path)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || linked.Update["curseforge"] == nil || linked.Download != mod.Download || linked.UpdateSource != mod.UpdateSource || !linked.Pin || !reflect.DeepEqual(linked.Environment, mod.Environment) || !reflect.DeepEqual(linked.Option, mod.Option) {
				t.Fatal("command failed to preserve primary/user settings")
			}
		})
	}
}
