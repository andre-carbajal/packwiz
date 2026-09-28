package cmdshared

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

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
	for p, v := range index.Files {
		if v.IsMetaFile() {
			continue
		}
		for _, aliasPath := range v.AliasPaths() {
			file, err := exp.Create(path.Join("overrides", aliasPath))
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
		}
	}
	return nil
}

func PrintDisclaimer(isCf bool) {
	fmt.Println("Disclaimer: you are responsible for ensuring you comply with ALL the licenses, or obtain appropriate permissions, for the files \"added to zip\" below")
	if isCf {
		fmt.Println("Note that mods bundled within a CurseForge pack must be in the Approved Non-CurseForge Mods list")
		fmt.Println("packwiz is currently unable to match metadata between mod sites - if any of these are available from CurseForge you should change them to use CurseForge metadata (e.g. by re-adding them using the cf commands)")
	} else {
		fmt.Println("packwiz is currently unable to match metadata between mod sites - if any of these are available from Modrinth you should change them to use Modrinth metadata (e.g. by re-adding them using the mr commands)")
	}
	fmt.Println()
}
