package modrinth

import (
	"archive/zip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const mrpackManifestName = "modrinth.index.json"

var mrpackWindowsDrive = regexp.MustCompile(`^[A-Za-z]:`)

var mrpackImportCmd = &cobra.Command{
	Use:   "import [modpack path]",
	Short: "Import a Modrinth .mrpack file into the current modpack",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return importMRPack(args[0])
	},
}

type mrpackOverride struct {
	file         *zip.File
	path         string
	instancePath string
	layer        string
}

type mrpackImportData struct {
	manifest  Pack
	files     []mrpackImportFile
	overrides []mrpackOverride
}

type mrpackImportFile struct {
	path        string
	fileName    string
	name        string
	url         string
	hash        string
	environment core.ModEnvironment
	update      map[string]map[string]any
	optional    bool
}

func readMRPack(reader io.ReaderAt, size int64) (mrpackImportData, error) {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return mrpackImportData{}, fmt.Errorf("invalid .mrpack ZIP: %w", err)
	}

	var manifestFile *zip.File
	seenNames := make(map[string]struct{}, len(archive.File))
	for _, file := range archive.File {
		isDir := file.FileInfo().IsDir()
		name := file.Name
		if isDir {
			name = strings.TrimSuffix(name, "/")
		}
		name, err := validateMRPackPath(name)
		if err != nil {
			return mrpackImportData{}, fmt.Errorf("invalid archive path %q: %w", file.Name, err)
		}
		if _, ok := seenNames[name]; ok {
			return mrpackImportData{}, fmt.Errorf("duplicate archive entry %q", name)
		}
		seenNames[name] = struct{}{}
		if !isDir && name == mrpackManifestName {
			manifestFile = file
		}
	}
	if manifestFile == nil {
		return mrpackImportData{}, fmt.Errorf("missing %s at the root of the .mrpack", mrpackManifestName)
	}

	manifestReader, err := manifestFile.Open()
	if err != nil {
		return mrpackImportData{}, fmt.Errorf("open %s: %w", mrpackManifestName, err)
	}
	manifestData, readErr := io.ReadAll(manifestReader)
	closeErr := manifestReader.Close()
	if readErr != nil {
		return mrpackImportData{}, fmt.Errorf("read %s: %w", mrpackManifestName, readErr)
	}
	if closeErr != nil {
		return mrpackImportData{}, fmt.Errorf("close %s: %w", mrpackManifestName, closeErr)
	}

	var manifest Pack
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return mrpackImportData{}, fmt.Errorf("parse %s: %w", mrpackManifestName, err)
	}
	data := mrpackImportData{manifest: manifest}
	if err := data.validateManifest(); err != nil {
		return mrpackImportData{}, err
	}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || file.Name == mrpackManifestName {
			continue
		}
		name, _ := validateMRPackPath(file.Name)
		var target string
		for _, layer := range []string{"overrides", "client-overrides", "server-overrides"} {
			prefix := layer + "/"
			if relative, ok := strings.CutPrefix(name, prefix); ok {
				if relative == "" {
					break
				}
				instancePath, pathErr := validateMRPackPath(relative)
				if pathErr != nil {
					return mrpackImportData{}, fmt.Errorf("invalid %s entry %q: %w", layer, file.Name, pathErr)
				}
				if layer == "overrides" {
					if instancePath == core.MRPackOverrideStorageDir || strings.HasPrefix(instancePath, core.MRPackOverrideStorageDir+"/") {
						return mrpackImportData{}, fmt.Errorf("override path %q uses Packwiz's reserved Modrinth layer storage", instancePath)
					}
					target = instancePath
				} else {
					target, err = validateMRPackPath(path.Join(core.MRPackOverrideStorageDir, layer, relative))
				}
				if err != nil {
					return mrpackImportData{}, fmt.Errorf("invalid %s entry %q: %w", layer, file.Name, err)
				}
				data.overrides = append(data.overrides, mrpackOverride{file: file, path: target, instancePath: instancePath, layer: layer})
				break
			}
		}
	}
	data.removeOverriddenDownloads()
	return data, nil
}

func (data *mrpackImportData) removeOverriddenDownloads() {
	paths := make(map[string]struct{})
	for _, override := range data.overrides {
		if override.layer == "overrides" {
			paths[override.instancePath] = struct{}{}
		}
	}
	files := data.files[:0]
	for _, file := range data.files {
		if _, overridden := paths[file.path]; !overridden {
			files = append(files, file)
		}
	}
	data.files = files
}

func (data *mrpackImportData) validateManifest() error {
	manifest := data.manifest
	if manifest.FormatVersion != 1 {
		return fmt.Errorf("unsupported .mrpack formatVersion %d (supported: 1)", manifest.FormatVersion)
	}
	if manifest.Game != "minecraft" {
		return fmt.Errorf("unsupported .mrpack game %q (supported: minecraft)", manifest.Game)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return errors.New(".mrpack name must not be empty")
	}
	if strings.TrimSpace(manifest.Dependencies["minecraft"]) == "" {
		return errors.New(".mrpack dependencies must include a Minecraft version")
	}

	seenPaths := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		filePath, err := validateMRPackPath(file.Path)
		if err != nil {
			return fmt.Errorf("invalid files[].path %q: %w", file.Path, err)
		}
		if strings.HasSuffix(filePath, core.MetaExtension) {
			return fmt.Errorf("files[].path %q uses Packwiz's reserved metadata extension", filePath)
		}
		if filePath == core.MRPackOverrideStorageDir || strings.HasPrefix(filePath, core.MRPackOverrideStorageDir+"/") {
			return fmt.Errorf("files[].path %q uses Packwiz's reserved Modrinth layer storage", filePath)
		}
		if _, ok := seenPaths[filePath]; ok {
			return fmt.Errorf("duplicate files[].path %q", filePath)
		}
		seenPaths[filePath] = struct{}{}

		sha1 := file.Hashes["sha1"]
		sha512 := file.Hashes["sha512"]
		if !validMRPackHash("sha1", sha1) || !validMRPackHash("sha512", sha512) {
			return fmt.Errorf("file %q must have valid SHA-1 and SHA-512 hashes", filePath)
		}

		var downloadURL string
		for _, candidate := range file.Downloads {
			if normalized, ok := validMRPackURL(candidate); ok {
				downloadURL = normalized
				break
			}
		}
		if downloadURL == "" {
			return fmt.Errorf("file %q has no valid HTTPS download URL", filePath)
		}

		env := PackEnvironment{Client: "required", Server: "required"}
		if file.Env != nil {
			env = *file.Env
		}
		if err := validatePackEnvironment(&env); err != nil {
			return fmt.Errorf("file %q: %w", filePath, err)
		}

		imported := mrpackImportFile{
			path:        filePath,
			fileName:    path.Base(filePath),
			name:        strings.TrimSuffix(path.Base(filePath), path.Ext(filePath)),
			url:         downloadURL,
			hash:        strings.ToLower(sha512),
			environment: core.ModEnvironment{Client: env.Client, Server: env.Server},
			optional:    env.Client == "optional" || env.Server == "optional",
		}
		if projectID, versionID, ok := modrinthIDsFromURL(downloadURL); ok {
			update, err := (mrUpdateData{ProjectID: projectID, InstalledVersion: versionID}).ToMap()
			if err != nil {
				return fmt.Errorf("prepare Modrinth update metadata for %q: %w", filePath, err)
			}
			imported.update = map[string]map[string]any{"modrinth": update}
		}
		data.files = append(data.files, imported)
	}
	return nil
}

func validateMRPackPath(value string) (string, error) {
	if value == "" || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") ||
		mrpackWindowsDrive.MatchString(value) {
		return "", errors.New("path must be relative and use forward slashes")
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path is not a normalized relative path")
	}
	return clean, nil
}

func validMRPackHash(format, value string) bool {
	implementation, err := core.GetHashImpl(format)
	if err != nil || len(value) != implementation.Size()*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == implementation.Size()
}

func validatePackEnvironment(env *PackEnvironment) error {
	for side, value := range map[string]string{"client": env.Client, "server": env.Server} {
		if value != "required" && value != "optional" && value != "unsupported" {
			return fmt.Errorf("environment %s must be required, optional, or unsupported", side)
		}
	}
	return nil
}

func validMRPackURL(value string) (string, bool) {
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	normalized, err := core.ReencodeURL(value)
	return normalized, err == nil
}

var modrinthIDPath = regexp.MustCompile(`^/data/([A-Za-z0-9]+)/versions/([A-Za-z0-9]+)/[^/]+$`)

func modrinthIDsFromURL(value string) (string, string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host != "cdn.modrinth.com" {
		return "", "", false
	}
	matches := modrinthIDPath.FindStringSubmatch(parsed.Path)
	if len(matches) != 3 {
		return "", "", false
	}
	return matches[1], matches[2], true
}

func importMRPack(filename string) error {
	archiveFile, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open .mrpack: %w", err)
	}
	defer archiveFile.Close()
	stat, err := archiveFile.Stat()
	if err != nil {
		return fmt.Errorf("read .mrpack metadata: %w", err)
	}
	data, err := readMRPack(archiveFile, stat.Size())
	if err != nil {
		return err
	}

	packFile := viper.GetString("pack-file")
	pack, err := core.LoadPack()
	newPack := false
	if err != nil {
		if _, statErr := os.Stat(packFile); statErr == nil {
			return fmt.Errorf("failed to load existing pack: %w", err)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to check existing pack: %w", statErr)
		}
		pack = newMRPack(data.manifest)
		newPack = true
	} else {
		if pack.Versions == nil {
			pack.Versions = make(map[string]string)
		}
		for name, version := range packVersions(data.manifest.Dependencies) {
			pack.Versions[name] = version
		}
	}

	packRoot, err := filepath.Abs(filepath.Dir(packFile))
	if err != nil {
		return fmt.Errorf("resolve pack directory: %w", err)
	}
	controlPaths := map[string]struct{}{}
	if abs, err := filepath.Abs(packFile); err == nil {
		controlPaths[filepath.Clean(abs)] = struct{}{}
	}
	indexPath := pack.Index.File
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(filepath.Dir(packFile), indexPath)
	}
	if abs, err := filepath.Abs(indexPath); err == nil {
		controlPaths[filepath.Clean(abs)] = struct{}{}
	}

	metadataPaths := make(map[string]struct{}, len(data.files))
	for i := range data.files {
		file := &data.files[i]
		destPath, err := safeMRPackOutputPath(packRoot, file.path)
		if err != nil {
			return err
		}
		metaRelative := file.path + core.MetaExtension
		metaPath, err := safeMRPackOutputPath(packRoot, metaRelative)
		if err != nil {
			return err
		}
		if err := rejectControlPath(destPath, controlPaths); err != nil {
			return fmt.Errorf("file %q: %w", file.path, err)
		}
		if err := rejectControlPath(metaPath, controlPaths); err != nil {
			return fmt.Errorf("metadata for %q: %w", file.path, err)
		}
		file.path = filepath.ToSlash(file.path)
		metadataPaths[filepath.Clean(metaPath)] = struct{}{}
	}
	for i := range data.overrides {
		override := &data.overrides[i]
		destPath, err := safeMRPackOutputPath(packRoot, override.path)
		if err != nil {
			return err
		}
		if err := rejectControlPath(destPath, controlPaths); err != nil {
			return fmt.Errorf("override %q: %w", override.path, err)
		}
		if _, collision := metadataPaths[filepath.Clean(destPath)]; collision {
			return fmt.Errorf("override %q would replace generated Packwiz metadata", override.path)
		}
	}

	if newPack {
		if err := createMRPackIndex(pack.Index.File, packFile); err != nil {
			return err
		}
	}
	index, err := pack.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	for _, file := range data.files {
		metaPath, err := safeMRPackOutputPath(packRoot, file.path+core.MetaExtension)
		if err != nil {
			return err
		}
		mod := core.Mod{
			Name:        file.name,
			FileName:    file.fileName,
			Side:        modSideFromEnvironment(file.environment),
			Environment: &file.environment,
			Download: core.ModDownload{
				URL:        file.url,
				HashFormat: "sha512",
				Hash:       file.hash,
			},
			Update: file.update,
		}
		if file.optional {
			mod.Option = &core.ModOption{Optional: true, Default: true}
		}
		mod.SetMetaPath(metaPath)
		if _, _, err := mod.Write(); err != nil {
			return fmt.Errorf("write metadata for %q: %w", file.path, err)
		}
	}

	for _, override := range data.overrides {
		destPath, err := safeMRPackOutputPath(packRoot, override.path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("create override directory for %q: %w", override.path, err)
		}
		dst, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("create override %q: %w", override.path, err)
		}
		src, err := override.file.Open()
		if err != nil {
			_ = dst.Close()
			return fmt.Errorf("open override %q: %w", override.path, err)
		}
		_, copyErr := io.Copy(dst, src)
		srcErr := src.Close()
		dstErr := dst.Close()
		if copyErr != nil {
			return fmt.Errorf("copy override %q: %w", override.path, copyErr)
		}
		if srcErr != nil {
			return fmt.Errorf("close override source %q: %w", override.path, srcErr)
		}
		if dstErr != nil {
			return fmt.Errorf("close override %q: %w", override.path, dstErr)
		}
	}

	if err := index.Refresh(); err != nil {
		return fmt.Errorf("refresh index: %w", err)
	}
	if err := index.Write(); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	if err := pack.UpdateIndexHash(); err != nil {
		return fmt.Errorf("update index hash: %w", err)
	}
	if err := pack.Write(); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	return nil
}

func newMRPack(manifest Pack) core.Pack {
	indexFile := viper.GetString("init.index-file")
	if indexFile == "" {
		indexFile = "index.toml"
	}
	return core.Pack{
		Name:        manifest.Name,
		Version:     manifest.VersionID,
		Description: manifest.Summary,
		PackFormat:  core.CurrentPackFormat,
		Index: struct {
			File       string `toml:"file"`
			HashFormat string `toml:"hash-format"`
			Hash       string `toml:"hash,omitempty"`
		}{File: indexFile},
		Versions: packVersions(manifest.Dependencies),
	}
}

func packVersions(dependencies map[string]string) map[string]string {
	versions := make(map[string]string, len(dependencies))
	for name, version := range dependencies {
		switch name {
		case "fabric-loader":
			name = "fabric"
		case "quilt-loader":
			name = "quilt"
		}
		versions[name] = version
	}
	return versions
}

func createMRPackIndex(indexFile, packFile string) error {
	indexPath := indexFile
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(filepath.Dir(packFile), indexPath)
	}
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}
	if _, err := os.Stat(indexPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check index file: %w", err)
	}
	if err := os.WriteFile(indexPath, []byte{}, 0o644); err != nil {
		return fmt.Errorf("create index file: %w", err)
	}
	return nil
}

func safeMRPackOutputPath(root, relative string) (string, error) {
	clean, err := validateMRPackPath(relative)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	current := root
	for _, component := range strings.Split(clean, "/") {
		current = filepath.Join(current, filepath.FromSlash(component))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("check output path %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path %q traverses a symlink inside the pack", relative)
		}
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the pack directory", relative)
	}
	return target, nil
}

func rejectControlPath(target string, controlPaths map[string]struct{}) error {
	if _, reserved := controlPaths[filepath.Clean(target)]; reserved {
		return errors.New("path would replace Packwiz metadata")
	}
	return nil
}

func modSideFromEnvironment(env core.ModEnvironment) string {
	if env.Client == "unsupported" && env.Server != "unsupported" {
		return core.ServerSide
	}
	if env.Server == "unsupported" && env.Client != "unsupported" {
		return core.ClientSide
	}
	return core.UniversalSide
}

func init() {
	modrinthCmd.AddCommand(mrpackImportCmd)
}
