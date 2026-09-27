package cmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andre-carbajal/packwiz/core"
)

func TestFilterModsByPinStatus(t *testing.T) {
	pinned := &core.Mod{Name: "pinned", Pin: true}
	unpinned := &core.Mod{Name: "unpinned"}
	mods := []*core.Mod{pinned, unpinned}

	cases := []struct {
		name         string
		showPinned   bool
		showUnpinned bool
		want         []*core.Mod
		wantErr      bool
	}{
		{name: "no filter", want: mods},
		{name: "pinned only", showPinned: true, want: []*core.Mod{pinned}},
		{name: "unpinned only", showUnpinned: true, want: []*core.Mod{unpinned}},
		{name: "mutually exclusive", showPinned: true, showUnpinned: true, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := filterModsByPinStatus(mods, tc.showPinned, tc.showUnpinned)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFormatListItem(t *testing.T) {
	base := &core.Mod{
		Name:     "Pretty Name",
		FileName: "pretty-name.jar",
		Download: core.ModDownload{URL: "https://cdn.modrinth.com/data/project/version/mod.jar"},
	}
	base.SetMetaPath(filepath.Join("mods", "nested", "pretty-name"+core.MetaExtension))
	legacy := &core.Mod{Name: "Legacy", FileName: "legacy.jar"}
	legacy.SetMetaPath(filepath.Join("mods", "legacy.toml"))
	curseForge := &core.Mod{Name: "CurseForge Mod", Download: core.ModDownload{Mode: core.ModeCF}}
	unknown := &core.Mod{Name: "Unknown Mod"}
	missingPath := &core.Mod{Name: "No Metadata Path"}

	cases := []struct {
		name         string
		mod          *core.Mod
		showVersion  bool
		showSlug     bool
		showProvider bool
		want         string
	}{
		{name: "name", mod: base, want: "Pretty Name"},
		{name: "version", mod: base, showVersion: true, want: "Pretty Name (pretty-name.jar)"},
		{name: "nested file slug", mod: base, showSlug: true, want: "pretty-name"},
		{name: "legacy toml slug", mod: legacy, showSlug: true, want: "legacy"},
		{name: "slug with provider", mod: base, showSlug: true, showProvider: true, want: "Modrinth: pretty-name"},
		{name: "curseforge provider", mod: curseForge, showProvider: true, want: "CurseForge: CurseForge Mod"},
		{name: "unknown provider", mod: unknown, showProvider: true, want: "Unknown: Unknown Mod"},
		{name: "missing metadata path", mod: missingPath, showSlug: true, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatListItem(tc.mod, tc.showVersion, tc.showSlug, tc.showProvider); got != tc.want {
				t.Errorf("formatListItem() = %q, want %q", got, tc.want)
			}
		})
	}
}
