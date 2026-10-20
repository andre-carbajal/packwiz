package cmdshared

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/andre-carbajal/packwiz/core"
)

func ListManualDownloads(session core.DownloadSession) error {
	manualDownloads := session.GetManualDownloads()
	if len(manualDownloads) > 0 {
		fmt.Printf("Found %v manual downloads; these mods are unable to be downloaded by packwiz (due to API limitations) and must be manually downloaded:\n",
			len(manualDownloads))
		for _, dl := range manualDownloads {
			fmt.Printf("%s (%s) from %s\n", dl.Name, dl.FileName, dl.URL)
		}
		cacheDir, err := core.GetPackwizCache()
		if err != nil {
			return fmt.Errorf("error locating cache folder: %w", err)
		}

		fmt.Printf("Once you have done so, place these files in %s and re-run this command.\n",
			filepath.Join(cacheDir, core.DownloadCacheImportFolder))
		return fmt.Errorf("waiting for %d manual download(s)", len(manualDownloads))
	}
	return nil
}

func AddToZip(dl core.CompletedDownload, exp *zip.Writer, dir string, index *core.Index) error {
	if dl.Error != nil {
		return fmt.Errorf("download of %s (%s) failed: %w", dl.Mod.Name, dl.Mod.FileName, dl.Error)
	}
	for _, warning := range dl.Warnings {
		fmt.Printf("Warning for %s (%s): %v\n", dl.Mod.Name, dl.Mod.FileName, warning)
	}

	p, err := index.RelIndexPath(dl.Mod.GetDestFilePath())
	if err != nil {
		return fmt.Errorf("error resolving external file: %w", err)
	}
	modFile, err := exp.Create(path.Join(dir, p))
	if err != nil {
		return fmt.Errorf("error creating metadata file %s: %w", p, err)
	}
	_, err = io.Copy(modFile, dl.File)
	if err != nil {
		return fmt.Errorf("error copying file %s: %w", p, err)
	}
	err = dl.File.Close()
	if err != nil {
		return fmt.Errorf("error closing file %s: %w", p, err)
	}

	fmt.Printf("%s (%s) added to zip\n", dl.Mod.Name, dl.Mod.FileName)
	return nil
}

// AddNonMetafileOverrides saves all non-metadata files into an overrides folder in the zip
func AddNonMetafileOverrides(index *core.Index, exp *zip.Writer) error {
	return addNonMetafileOverrides(index, exp, false)
}

// AddModrinthOverrides also restores Modrinth's client/server override layers
// stored under .packwiz-mrpack in the source pack.
func AddModrinthOverrides(index *core.Index, exp *zip.Writer) error {
	return addNonMetafileOverrides(index, exp, true)
}

func addNonMetafileOverrides(index *core.Index, exp *zip.Writer, modrinthLayers bool) error {
	written := make(map[string]struct{})
	for p, v := range index.Files {
		if v.IsMetaFile() {
			continue
		}
		for _, aliasPath := range v.AliasPaths() {
			if aliasPath == core.MRPackOverrideStorageDir || strings.HasPrefix(aliasPath, core.MRPackOverrideStorageDir+"/") {
				continue
			}
			archivePath := path.Join("overrides", aliasPath)
			if _, duplicate := written[archivePath]; duplicate {
				continue
			}
			file, err := exp.Create(archivePath)
			if err != nil {
				return fmt.Errorf("error creating override %s: %w", aliasPath, err)
			}
			// Attempt to read the file from disk, without checking hashes (assumed to have no errors)
			src, err := os.Open(index.ResolveIndexPath(p))
			if err != nil {
				return fmt.Errorf("error reading override %s: %w", p, err)
			}
			_, err = io.Copy(file, src)
			if err != nil {
				_ = src.Close()
				return fmt.Errorf("error copying override %s: %w", p, err)
			}

			if err := src.Close(); err != nil {
				return fmt.Errorf("error closing override %s: %w", p, err)
			}
			written[archivePath] = struct{}{}
		}
	}
	return addStoredMRPackOverrides(index, exp, modrinthLayers, written)
}

func addStoredMRPackOverrides(index *core.Index, exp *zip.Writer, modrinthLayers bool, written map[string]struct{}) error {
	root := index.ResolveIndexPath(".")
	for _, layer := range []string{"client-overrides", "server-overrides"} {
		storedRoot := filepath.Join(root, core.MRPackOverrideStorageDir, layer)
		info, err := os.Lstat(storedRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("check stored %s: %w", layer, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("stored %s path is not a directory", layer)
		}
		err = filepath.WalkDir(storedRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filePath == storedRoot || entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("stored override %q is a symlink", filePath)
			}
			fileInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if !fileInfo.Mode().IsRegular() {
				return fmt.Errorf("stored override %q is not a regular file", filePath)
			}
			relative, err := filepath.Rel(storedRoot, filePath)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			archivePath := path.Join("overrides", relative)
			if modrinthLayers {
				archivePath = path.Join(layer, relative)
			}
			if _, duplicate := written[archivePath]; duplicate {
				return nil
			}
			output, err := exp.Create(archivePath)
			if err != nil {
				return fmt.Errorf("error creating override %s: %w", archivePath, err)
			}
			source, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("error reading override %s: %w", filePath, err)
			}
			if _, err := io.Copy(output, source); err != nil {
				_ = source.Close()
				return fmt.Errorf("error copying override %s: %w", filePath, err)
			}
			if err := source.Close(); err != nil {
				return fmt.Errorf("error closing override %s: %w", filePath, err)
			}
			written[archivePath] = struct{}{}
			return nil
		})
		if err != nil {
			return fmt.Errorf("read stored %s overrides: %w", layer, err)
		}
	}
	return nil
}

func PrintDisclaimer(isCf bool) {
	fmt.Println("Disclaimer: you are responsible for ensuring you comply with ALL the licenses, or obtain appropriate permissions, for the files \"added to zip\" below")
	if isCf {
		fmt.Println("Note that mods bundled within a CurseForge pack must be in the Approved Non-CurseForge Mods list")
		fmt.Println("Use packwiz link --all to associate identical files hosted on CurseForge.")
	} else {
		fmt.Println("Use packwiz link --all to associate identical files hosted on Modrinth.")
	}
	fmt.Println()
}
