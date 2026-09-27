package cmd

import (
	"reflect"
	"testing"

	"github.com/andre-carbajal/packwiz/core"
)

func TestExistingPackLoaderDefaults(t *testing.T) {
	cases := []struct {
		name         string
		pack         core.Pack
		wantLoader   string
		wantVersions map[string]string
	}{
		{
			name: "single loader",
			pack: core.Pack{Versions: map[string]string{
				"minecraft": "1.20.1",
				"fabric":    "0.16.0",
			}},
			wantLoader:   "fabric",
			wantVersions: map[string]string{"fabric": "0.16.0"},
		},
		{
			name: "multiple loaders are preserved with stable selection",
			pack: core.Pack{Versions: map[string]string{
				"minecraft": "1.20.1",
				"forge":     "47.3.0",
				"fabric":    "0.16.0",
			}},
			wantLoader:   "fabric",
			wantVersions: map[string]string{"fabric": "0.16.0", "forge": "47.3.0"},
		},
		{
			name:         "no loader preserves other version metadata",
			pack:         core.Pack{Versions: map[string]string{"minecraft": "1.20.1", "custom": "value"}},
			wantLoader:   "none",
			wantVersions: map[string]string{"custom": "value"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader, versions := existingPackLoaderDefaults(tc.pack)
			if loader != tc.wantLoader {
				t.Errorf("loader = %q, want %q", loader, tc.wantLoader)
			}
			if !reflect.DeepEqual(versions, tc.wantVersions) {
				t.Errorf("versions = %v, want %v", versions, tc.wantVersions)
			}
		})
	}
}
