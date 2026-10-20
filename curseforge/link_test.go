package curseforge

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/jarcoal/httpmock"
	"github.com/spf13/viper"
)

func cfTestIdentity(data string) core.FileIdentity {
	return core.FileIdentity{Bytes: []byte(data), Hashes: map[string]string{"sha1": fmt.Sprintf("%x", sha1.Sum([]byte(data))), "sha512": fmt.Sprintf("%x", sha512.Sum512([]byte(data)))}}
}

func TestLinkCurseForge(t *testing.T) {
	identity := cfTestIdentity("abc")
	for _, tc := range []struct {
		name, hash    string
		wantErrText   string
		count, status int
		reference     string
		wantErr       bool
		wantAuth      bool
	}{
		{name: "exact SHA", hash: identity.Hashes["sha1"], count: 1, status: 200},
		{name: "fingerprint only mismatch", hash: cfTestIdentity("a b c").Hashes["sha1"], count: 1, status: 200, wantErr: true},
		{name: "no matches", count: 0, status: 200, wantErr: true, wantErrText: "identical contents"},
		{name: "ambiguous", hash: identity.Hashes["sha1"], count: 2, status: 200, wantErr: true},
		{name: "API error", status: 503, wantErr: true},
		{name: "invalid API key", status: 401, wantErr: true, wantAuth: true},
		{name: "manual project", hash: identity.Hashes["sha1"], count: 1, status: 200, reference: "https://www.curseforge.com/minecraft/mc-mods/example"},
		{name: "manual file disambiguates", hash: identity.Hashes["sha1"], count: 2, status: 200, reference: "https://www.curseforge.com/minecraft/mc-mods/example/files/10"},
		{name: "wrong manual file", hash: identity.Hashes["sha1"], count: 1, status: 200, reference: "https://www.curseforge.com/minecraft/mc-mods/example/files/55", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestCFAPIKey(t)
			httpmock.ActivateNonDefault(cfDefaultClient.httpClient)
			defer httpmock.DeactivateAndReset()
			var matches []any
			for i := 0; i < tc.count; i++ {
				matches = append(matches, map[string]any{"id": 999, "file": map[string]any{"modId": 1, "id": 10 + i, "hashes": []any{map[string]any{"algo": 1, "value": tc.hash}}}})
			}
			httpmock.RegisterResponder("POST", "https://api.curseforge.com/v1/fingerprints", httpmock.NewJsonResponderOrPanic(tc.status, map[string]any{"data": map[string]any{"exactMatches": matches}}))
			httpmock.RegisterRegexpResponder("GET", regexp.MustCompile(`^https://api\.curseforge\.com/v1/mods/search\?`), httpmock.NewStringResponder(200, `{"data":[{"id":1,"slug":"example"}]}`))
			got, err := linkCurseForge(identity, tc.reference)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %v, %v", got, err)
			}
			if tc.wantErrText != "" && !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("error %q does not explain %q", err, tc.wantErrText)
			}
			var authErr *core.PlatformAuthError
			if errors.As(err, &authErr) != tc.wantAuth {
				t.Fatalf("auth error = %v, wantAuth %v", err, tc.wantAuth)
			}
			if !tc.wantErr {
				parsed, err := (cfUpdater{}).ParseUpdate(got)
				if err != nil {
					t.Fatal(err)
				}
				data := parsed.(cfUpdateData)
				if data.ProjectID != 1 || data.FileID != 10 {
					t.Fatalf("wrong reference: %+v", data)
				}
			}
		})
	}
}

func TestCurseForgeFingerprintNeedsFreshByteVerification(t *testing.T) {
	// Same fingerprint, different bytes: a cached original cannot prove that
	// the candidate is identical when CurseForge omits SHA-1.
	identity := cfTestIdentity("abc")
	if getByteArrayHash(identity.Bytes) != getByteArrayHash([]byte("a b c")) {
		t.Fatal("fixture must collide after whitespace normalization")
	}
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	for _, data := range []string{"abc", "a b c"} {
		httpmock.RegisterResponder("GET", "https://example.com/candidate.jar", httpmock.NewStringResponder(200, data))
		got, err := identicalCFFile(identity, 1, modFileInfo{ID: 10, DownloadURL: "https://example.com/candidate.jar"})
		if err != nil || got != (data == "abc") {
			t.Fatalf("data %q: %v, %v", data, got, err)
		}
	}
}

func TestCurseForgeUpdateReplacesSecondaryReference(t *testing.T) {
	setTestCFAPIKey(t)
	identity := cfTestIdentity("updated artifact")
	var file modFileInfo
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":20,"modId":1,"fileName":"new.jar","downloadUrl":"https://example.com/new.jar","hashes":[{"algo":1,"value":%q}]}`, identity.Hashes["sha1"])), &file); err != nil {
		t.Fatal(err)
	}
	mod := core.Mod{Name: "Update", FileName: "old.jar", UpdateSource: "curseforge", Environment: &core.ModEnvironment{Client: "required", Server: "unsupported"}, Download: core.ModDownload{Mode: core.ModeCF, HashFormat: "sha1", Hash: cfTestIdentity("old").Hashes["sha1"]}, Update: map[string]map[string]any{"curseforge": {"project-id": uint32(1), "file-id": uint32(10)}, "modrinth": {"mod-id": "Project1", "version": "Version1"}}}
	oldCache := viper.Get("cache.directory")
	viper.Set("cache.directory", t.TempDir())
	t.Cleanup(func() { viper.Set("cache.directory", oldCache) })
	httpmock.Activate()
	httpmock.ActivateNonDefault(cfDefaultClient.httpClient)
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("POST", "https://api.curseforge.com/v1/mods/files", httpmock.NewJsonResponderOrPanic(200, map[string]any{"data": []modFileInfo{file}}))
	httpmock.RegisterResponder("GET", file.DownloadURL, httpmock.NewStringResponder(200, string(identity.Bytes)))
	version := map[string]any{"id": "Version2", "project_id": "Project1", "files": []any{map[string]any{"hashes": identity.Hashes}}}
	httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/version_file/"+identity.Hashes["sha512"]+"?algorithm=sha512", httpmock.NewJsonResponderOrPanic(200, version))
	httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/project/Project1/version", httpmock.NewJsonResponderOrPanic(200, []any{version}))
	if err := (cfUpdater{}).DoUpdate([]*core.Mod{&mod}, []any{cachedStateStore{modInfo: modInfo{ID: 1, Name: "Update"}, fileID: 20, fileInfo: &file}}); err != nil {
		t.Fatal(err)
	}
	if mod.Update["modrinth"]["version"] != "Version2" || mod.UpdateSource != "curseforge" || mod.Download.Mode != core.ModeCF || mod.Environment.Server != "unsupported" {
		t.Fatalf("stale reference/settings: %+v", mod)
	}
}

func TestAddingCurseForgeFileAutomaticallyLinksModrinth(t *testing.T) {
	setTestCFAPIKey(t)
	identity := cfTestIdentity("artifact")
	_, _ = cfExportFixture(t, false)
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
	var file modFileInfo
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":10,"modId":1,"fileName":"file.jar","downloadUrl":"https://example.com/file.jar","hashes":[{"algo":1,"value":%q}]}`, identity.Hashes["sha1"])), &file); err != nil {
		t.Fatal(err)
	}
	httpmock.Activate()
	httpmock.ActivateNonDefault(cfDefaultClient.httpClient)
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("POST", "https://api.curseforge.com/v1/mods/files", httpmock.NewJsonResponderOrPanic(200, map[string]any{"data": []modFileInfo{file}}))
	httpmock.RegisterResponder("GET", file.DownloadURL, httpmock.NewStringResponder(200, "artifact"))
	version := map[string]any{"id": "Version1", "project_id": "Project1", "files": []any{map[string]any{"hashes": identity.Hashes}}}
	httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/version_file/"+identity.Hashes["sha512"]+"?algorithm=sha512", httpmock.NewJsonResponderOrPanic(200, version))
	httpmock.RegisterResponder("GET", "https://api.modrinth.com/v2/project/Project1/version", httpmock.NewJsonResponderOrPanic(200, []any{version}))
	if err := createModFile(modInfo{ID: 1, Name: "Example", Slug: "example", GameID: 432, ClassID: 6}, file, &index, false); err != nil {
		t.Fatal(err)
	}
	installed, err := core.LoadMod(filepath.Join(root, "mods", "example.pw.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if installed.Update["modrinth"] == nil || installed.UpdateSource != "curseforge" || installed.Download.Mode != core.ModeCF {
		t.Fatalf("primary/link not preserved: %+v", installed)
	}
}
