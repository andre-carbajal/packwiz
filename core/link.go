package core

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// PlatformAuthError marks credentials rejected by a platform so batch linking
// can stop retrying the same permanent failure for every mod.
type PlatformAuthError struct{ Platform string }

func (e *PlatformAuthError) Error() string {
	return fmt.Sprintf("%s rejected API authentication (401 Unauthorized)", e.Platform)
}

// FileIdentity is computed from the installed artifact, not its name/version.
type FileIdentity struct {
	// ponytail: one artifact in memory for CF fingerprints; stream normalization
	// if unusually large artifacts make memory pressure measurable.
	Bytes  []byte
	Hashes map[string]string
}

// PlatformLinkers are registered by each platform to avoid package cycles.
// An empty reference requests detection; a URL restricts it to that project/file.
var PlatformLinkers = make(map[string]func(FileIdentity, string) (map[string]any, error))

func (m Mod) PrimaryUpdateSource() (string, error) {
	if m.UpdateSource != "" {
		if _, ok := m.Update[m.UpdateSource]; !ok {
			return "", fmt.Errorf("update-source %q has no metadata for %s", m.UpdateSource, m.Name)
		}
		if _, ok := Updaters[m.UpdateSource]; !ok {
			return "", fmt.Errorf("unsupported update-source %q", m.UpdateSource)
		}
		return m.UpdateSource, nil
	}
	if len(m.Update) == 1 {
		for source := range m.Update {
			if _, ok := Updaters[source]; ok {
				return source, nil
			}
		}
	}
	source := ""
	if m.Download.Mode == ModeCF {
		source = "curseforge"
	}
	if m.Download.Mode == "" || m.Download.Mode == ModeURL {
		if u, err := url.Parse(m.Download.URL); err == nil && u.Hostname() == "cdn.modrinth.com" {
			source = "modrinth"
		}
	}
	if _, ok := m.Update[source]; source != "" && ok {
		return source, nil
	}
	return "", fmt.Errorf("cannot determine update-source for %s; set it explicitly in %s", m.Name, m.GetFilePath())
}

// ReadFileIdentity reuses the download cache and its integrity checks.
func ReadFileIdentity(m *Mod) (FileIdentity, error) {
	if err := m.ParseUpdateData(); err != nil {
		return FileIdentity{}, err
	}
	if m.Download.HashFormat == "murmur2" {
		return readFingerprintIdentity(m)
	}
	session, err := CreateDownloadSession([]*Mod{m}, []string{"sha1", "sha512"})
	if err != nil {
		return FileIdentity{}, err
	}
	if len(session.GetManualDownloads()) > 0 {
		return FileIdentity{}, fmt.Errorf("%s requires a manual download into the packwiz cache", m.Name)
	}
	var identity FileIdentity
	for dl := range session.StartDownloads() {
		if dl.Error != nil {
			return FileIdentity{}, dl.Error
		}
		identity.Bytes, err = io.ReadAll(dl.File)
		closeErr := dl.File.Close()
		if err != nil {
			return FileIdentity{}, err
		}
		if closeErr != nil {
			return FileIdentity{}, closeErr
		}
		identity.Hashes = dl.Hashes
	}
	if err := session.SaveIndex(); err != nil {
		return FileIdentity{}, err
	}
	return identity, nil
}

// A fingerprint cache hit is not proof of the primary file's bytes either.
func readFingerprintIdentity(m *Mod) (FileIdentity, error) {
	var reader io.ReadCloser
	if m.Download.Mode == ModeCF {
		downloader, ok := MetaDownloaders["curseforge"]
		if !ok {
			return FileIdentity{}, fmt.Errorf("CurseForge downloader is unavailable")
		}
		metadata, err := downloader.GetFilesMetadata([]*Mod{m})
		if err != nil {
			return FileIdentity{}, err
		}
		if len(metadata) != 1 || metadata[0] == nil {
			return FileIdentity{}, fmt.Errorf("missing primary file metadata")
		}
		if manual, _ := metadata[0].GetManualDownload(); manual {
			return FileIdentity{}, fmt.Errorf("cannot prove byte identity for a manual-download file with only a fingerprint")
		}
		reader, err = metadata[0].DownloadFile()
		if err != nil {
			return FileIdentity{}, err
		}
	} else if m.Download.Mode == "" || m.Download.Mode == ModeURL {
		response, err := GetWithUA(m.Download.URL, "application/octet-stream")
		if err != nil {
			return FileIdentity{}, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return FileIdentity{}, fmt.Errorf("download returned %s", response.Status)
		}
		reader = response.Body
	} else {
		return FileIdentity{}, fmt.Errorf("cannot verify fingerprint for download mode %q", m.Download.Mode)
	}
	defer reader.Close()
	var output bytes.Buffer
	hashes := map[string]string{m.Download.HashFormat: m.Download.Hash}
	if err := teeHashes([]string{"sha1", "sha512"}, hashes, &output, reader); err != nil {
		return FileIdentity{}, err
	}
	return FileIdentity{Bytes: output.Bytes(), Hashes: hashes}, nil
}

func LinkMod(m *Mod, target, reference string) error {
	linker, ok := PlatformLinkers[target]
	if !ok {
		return fmt.Errorf("unsupported link platform %q", target)
	}
	identity, err := ReadFileIdentity(m)
	if err != nil {
		return err
	}
	metadata, err := linker(identity, reference)
	if err != nil {
		return err
	}
	updater, ok := Updaters[target]
	if !ok {
		return fmt.Errorf("unsupported update platform %q", target)
	}
	parsed, err := updater.ParseUpdate(metadata)
	if err != nil {
		return err
	}
	if m.Update == nil {
		m.Update = make(map[string]map[string]any)
	}
	m.Update[target] = metadata
	m.updateData[target] = parsed
	return nil
}

// AutoLinkMod never fails the primary installation/update.
func AutoLinkMod(m *Mod) {
	source, err := m.PrimaryUpdateSource()
	if err != nil {
		fmt.Printf("Warning: %s\n", err)
		return
	}
	m.UpdateSource = source
	target := ""
	switch source {
	case "curseforge":
		target = "modrinth"
	case "modrinth":
		target = "curseforge"
	default:
		return
	}
	if _, ok := m.Update[target]; ok {
		return
	}
	if _, ok := PlatformLinkers[target]; !ok {
		return
	}
	if err := LinkMod(m, target, ""); err != nil {
		fmt.Printf("Warning: could not link %s to %s: %s\n", m.Name, target, err)
	}
}

// ResetSecondaryReferences must run only after a successful primary update.
func ResetSecondaryReferences(m *Mod, source string) {
	m.UpdateSource = source
	for _, target := range []string{"curseforge", "modrinth"} {
		if target != source {
			delete(m.Update, target)
			delete(m.updateData, target)
		}
	}
	AutoLinkMod(m)
}

func ValidatePlatformReferences(mods []*Mod, target string, allowOverrides bool) error {
	var missing []string
	for _, mod := range mods {
		if _, ok := mod.GetParsedUpdateData(target); !ok {
			missing = append(missing, mod.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	message := fmt.Sprintf("missing %s references: %s", target, strings.Join(missing, ", "))
	if !allowOverrides {
		return fmt.Errorf("%s; run packwiz link --all or explicitly use --allow-overrides", message)
	}
	fmt.Printf("Warning: %s; external files may be rejected by the platform.\n", message)
	return nil
}

// WriteInstalledMod reuses an existing project's path even when platform slugs
// differ, and keeps user options instead of replacing them with defaults.
// ponytail: one index scan per add; build a project lookup if large dependency
// batches make this O(n²) work measurable.
func WriteInstalledMod(mod *Mod, index *Index) error {
	mods, err := index.LoadAllMods()
	if err != nil {
		return err
	}
	var existing *Mod
	for _, installed := range mods {
		matched := installed.GetFilePath() == mod.GetFilePath()
		for _, platform := range []string{"curseforge", "modrinth"} {
			key := "project-id"
			if platform == "modrinth" {
				key = "mod-id"
			}
			a, aok := installed.Update[platform][key]
			b, bok := mod.Update[platform][key]
			if aok && bok && fmt.Sprint(a) == fmt.Sprint(b) {
				matched = true
			}
		}
		if !matched {
			continue
		}
		if existing != nil {
			return fmt.Errorf("multiple existing metadata files match %s; remove duplicates before adding", mod.Name)
		}
		existing = installed
	}
	if existing != nil {
		mod.SetMetaPath(existing.GetFilePath())
		mod.Option, mod.Environment, mod.Side, mod.Pin = existing.Option, existing.Environment, existing.Side, existing.Pin
	}
	format, hash, err := mod.Write()
	if err != nil {
		return err
	}
	return index.RefreshFileWithHash(mod.GetFilePath(), format, hash, true)
}
