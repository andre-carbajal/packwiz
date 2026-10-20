package modrinth

import (
	"fmt"
	"net/url"
	"strings"

	modrinthApi "codeberg.org/theepicblock/go-modrinth/modrinth"
	"github.com/andre-carbajal/packwiz/core"
)

func init() { core.PlatformLinkers["modrinth"] = linkModrinth }

func linkModrinth(identity core.FileIdentity, reference string) (map[string]any, error) {
	var versions []*modrinthApi.Version
	filename := ""
	if reference == "" {
		version, err := mrDefaultClient.VersionFiles.GetFromHash(identity.Hashes["sha512"], "sha512")
		if err != nil {
			return nil, err
		}
		if version == nil || version.ProjectID == nil || version.ID == nil {
			return nil, fmt.Errorf("invalid Modrinth hash response")
		}
		versions, err = mrDefaultClient.Versions.ListVersions(*version.ProjectID, &modrinthApi.ListVersionsOptions{})
		if err != nil {
			return nil, err
		}
		// The hash endpoint may expose an unlisted version absent from the list.
		versions = append(versions, version)
	} else {
		u, err := url.Parse(reference)
		if err != nil || u.Scheme != "https" || (u.Hostname() != "modrinth.com" && u.Hostname() != "www.modrinth.com" && u.Hostname() != "cdn.modrinth.com") {
			return nil, fmt.Errorf("expected a Modrinth HTTPS project, version or file URL")
		}
		var slug, versionNumber, versionID string
		if _, err := parseSlugOrUrl(reference, &slug, &versionNumber, &versionID, &filename); err != nil {
			return nil, err
		}
		if slug == "" {
			return nil, fmt.Errorf("invalid Modrinth URL")
		}
		project, err := mrDefaultClient.Projects.Get(slug)
		if err != nil {
			return nil, err
		}
		if project.ID == nil {
			return nil, fmt.Errorf("invalid Modrinth project response")
		}
		if versionID != "" {
			version, err := mrDefaultClient.Versions.Get(versionID)
			if err != nil {
				return nil, err
			}
			versions = []*modrinthApi.Version{version}
		} else {
			versions, err = mrDefaultClient.Versions.ListVersions(*project.ID, &modrinthApi.ListVersionsOptions{})
			if err != nil {
				return nil, err
			}
		}
		filtered := versions[:0]
		for _, version := range versions {
			if version == nil || version.ProjectID == nil || *version.ProjectID != *project.ID {
				continue
			}
			if versionNumber != "" && (version.ID == nil || *version.ID != versionNumber) && (version.VersionNumber == nil || *version.VersionNumber != versionNumber) {
				continue
			}
			filtered = append(filtered, version)
		}
		versions = filtered
	}
	var match *modrinthApi.Version
	for _, version := range versions {
		if version == nil || version.ID == nil || version.ProjectID == nil {
			return nil, fmt.Errorf("invalid Modrinth version response")
		}
		for _, file := range version.Files {
			if filename != "" && (file == nil || file.Filename == nil || *file.Filename != filename) {
				continue
			}
			if identicalMRFile(identity.Hashes, file) {
				if match != nil && *match.ID != *version.ID {
					return nil, fmt.Errorf("multiple identical Modrinth versions; use --modrinth <version-URL>")
				}
				match = version
			}
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no Modrinth file with identical contents was found; linking requires the exact same file, so this mod was left unchanged")
	}
	return (mrUpdateData{ProjectID: *match.ProjectID, InstalledVersion: *match.ID}).ToMap()
}

func identicalMRFile(hashes map[string]string, file *modrinthApi.File) bool {
	if file == nil {
		return false
	}
	for _, format := range []string{"sha512", "sha1"} {
		if hashes[format] != "" && file.Hashes[format] != "" {
			return strings.EqualFold(hashes[format], file.Hashes[format])
		}
	}
	return false
}

// platformExportMod resolves a native URL on a copy, leaving installed metadata
// and the primary updater untouched, including CurseForge's URL caching rules.
func platformExportMod(mod *core.Mod) (*core.Mod, error) {
	raw, ok := mod.GetParsedUpdateData("modrinth")
	if !ok {
		return mod, nil
	}
	data := raw.(mrUpdateData)
	version, err := mrDefaultClient.Versions.Get(data.InstalledVersion)
	if err != nil {
		return nil, err
	}
	if version == nil || version.ID == nil || *version.ID != data.InstalledVersion || version.ProjectID == nil || *version.ProjectID != data.ProjectID {
		return nil, fmt.Errorf("Modrinth version does not belong to the linked project/version")
	}
	hashes := map[string]string{mod.Download.HashFormat: mod.Download.Hash}
	if mod.Download.HashFormat != "sha1" && mod.Download.HashFormat != "sha512" {
		identity, err := core.ReadFileIdentity(mod)
		if err != nil {
			return nil, err
		}
		hashes = identity.Hashes
	}
	var matched *modrinthApi.File
	for _, file := range version.Files {
		if !identicalMRFile(hashes, file) {
			continue
		}
		if file.URL == nil || file.Size == nil || file.Hashes["sha1"] == "" || file.Hashes["sha512"] == "" {
			continue
		}
		// Different names for the same bytes in one version are not different
		// artifacts. Prefer the installed name, otherwise a stable native URL.
		if matched == nil || (file.Filename != nil && *file.Filename == mod.FileName && (matched.Filename == nil || *matched.Filename != mod.FileName)) ||
			((matched.Filename == nil || *matched.Filename != mod.FileName) && *file.URL < *matched.URL) {
			matched = file
		}
	}
	if matched == nil || matched.URL == nil || matched.Size == nil || matched.Hashes["sha1"] == "" || matched.Hashes["sha512"] == "" {
		return nil, fmt.Errorf("linked Modrinth version has no identical downloadable file for %s", mod.Name)
	}
	u, err := url.Parse(*matched.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "cdn.modrinth.com" {
		return nil, fmt.Errorf("invalid native Modrinth download URL")
	}
	copy := *mod
	format, hash := "sha512", hashes["sha512"]
	if hash == "" {
		format, hash = "sha1", hashes["sha1"]
	}
	// Validate downloads against the installed artifact, not an unrelated hash
	// supplied by the secondary API alongside its matching SHA-1.
	copy.Download = core.ModDownload{URL: *matched.URL, HashFormat: format, Hash: hash}
	return &copy, nil
}
