package cmd

import (
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
