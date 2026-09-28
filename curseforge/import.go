package curseforge

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/andre-carbajal/packwiz/curseforge/packinterop"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// importCmd represents the import command
var importCmd = &cobra.Command{
	Use:   "import [modpack path]",
	Short: "Import a curseforge modpack from a downloaded pack zip or an installed metadata json file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inputFile := args[0]
		var packImport packinterop.ImportPackMetadata

		// TODO: refactor/extract file checking?
		if strings.HasPrefix(inputFile, "http") {
			// TODO: implement
			return errors.New("HTTP import is not supported yet")
		} else {
			// Attempt to read from file
			var f *os.File
			inputFileStat, err := os.Stat(inputFile)
			if err == nil && inputFileStat.IsDir() {
				// Apparently os.Open doesn't fail when file given is a directory, only when it gets read
				err = errors.New("cannot open directory")
			}
			if err == nil {
				f, err = os.Open(inputFile)
			}
			if err != nil {
				found := false
				var errInstance error
				var errManifest error
				var errCurse error

				// Look for other files/folders
				if _, errInstance = os.Stat(filepath.Join(inputFile, "minecraftinstance.json")); errInstance == nil {
					inputFile = filepath.Join(inputFile, "minecraftinstance.json")
					found = true
				} else if _, errManifest = os.Stat(filepath.Join(inputFile, "manifest.json")); errManifest == nil {
					inputFile = filepath.Join(inputFile, "manifest.json")
					found = true
				} else if runtime.GOOS == "windows" {
					var dir string
					dir, errCurse = getCurseDir()
					if errCurse == nil {
						curseInstanceFile := filepath.Join(dir, "Minecraft", "Instances", inputFile, "minecraftinstance.json")
						if _, errCurse = os.Stat(curseInstanceFile); errCurse == nil {
							inputFile = curseInstanceFile
							found = true
						}
					}
				}

				if found {
					f, err = os.Open(inputFile)
					if err != nil {
						return fmt.Errorf("error opening file: %w", err)
					}
				} else {
					fmt.Printf("Error opening file: %s\n", err)
					fmt.Printf("Also attempted minecraftinstance.json: %s\n", errInstance)
					fmt.Printf("Also attempted manifest.json: %s\n", errManifest)
					if errCurse != nil {
						fmt.Printf("Also attempted to load a Curse/Twitch modpack named \"%s\": %s\n", inputFile, errCurse)
					}
					return fmt.Errorf("error opening file %q: %w", inputFile, err)
				}
			}
			defer f.Close()

			buf := bufio.NewReader(f)
			header, err := buf.Peek(2)
			if err != nil {
				return fmt.Errorf("error reading file: %w", err)
			}

			// Check if file is a zip
			if string(header) == "PK" {
				// Read the whole file (as bufio doesn't work for zips)
				zipData, err := io.ReadAll(buf)
				if err != nil {
					return fmt.Errorf("error reading file: %w", err)
				}
				// Get zip size
				stat, err := f.Stat()
				if err != nil {
					return fmt.Errorf("error reading file metadata: %w", err)
				}
				zr, err := zip.NewReader(bytes.NewReader(zipData), stat.Size())
				if err != nil {
					return fmt.Errorf("error parsing zip: %w", err)
				}

				// Search the zip for minecraftinstance.json or manifest.json
				var metaFile *zip.File
				for _, v := range zr.File {
					if v.Name == "minecraftinstance.json" || v.Name == "manifest.json" {
						metaFile = v
					}
				}

				if metaFile == nil {
					return errors.New("can't find manifest.json or minecraftinstance.json; is this a valid pack?")
				}

				packImport, err = packinterop.ReadMetadata(packinterop.GetZipPackSource(metaFile, zr))
			} else {
				packImport, err = packinterop.ReadMetadata(packinterop.GetDiskPackSource(buf, filepath.ToSlash(filepath.Base(inputFile)), filepath.Dir(inputFile)))
			}
			if err != nil {
				return fmt.Errorf("failed to read pack metadata: %w", err)
			}
		}

		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println("Failed to load existing pack, creating a new one...")

			// Create a new modpack
			indexFilePath := viper.GetString("init.index-file")
			_, err = os.Stat(indexFilePath)
			if os.IsNotExist(err) {
				// Create file
				err = os.WriteFile(indexFilePath, []byte{}, 0644)
				if err != nil {
					return fmt.Errorf("error creating index file: %w", err)
				}
				fmt.Println(indexFilePath + " created!")
			} else if err != nil {
				return fmt.Errorf("error checking index file: %w", err)
			}

			pack = core.Pack{
				Name:       packImport.Name(),
				Author:     packImport.PackAuthor(),
				Version:    packImport.PackVersion(),
				PackFormat: core.CurrentPackFormat,
				Index: struct {
					File       string `toml:"file"`
					HashFormat string `toml:"hash-format"`
					Hash       string `toml:"hash,omitempty"`
				}{
					File: indexFilePath,
				},
				Versions: packImport.Versions(),
			}
		} else {
			for component, version := range packImport.Versions() {
				packVersion, ok := pack.Versions[component]
				if !ok {
					fmt.Println("Set " + core.ComponentToFriendlyName(component) + " version to " + version)
				} else if packVersion != version {
					fmt.Println("Set " + core.ComponentToFriendlyName(component) + " version to " + version + " (previously " + packVersion + ")")
				}
				pack.Versions[component] = version
			}
		}
		index, err := pack.LoadIndex()
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		modsList := packImport.Mods()
		modIDs := make([]uint32, len(modsList))
		for i, v := range modsList {
			modIDs[i] = v.ProjectID
		}

		fmt.Println("Querying Curse API for dependency info...")

		modInfos, err := cfDefaultClient.getModInfoMultiple(modIDs)
		if err != nil {
			return fmt.Errorf("failed to obtain project information: %w", err)
		}

		modInfosMap := make(map[uint32]modInfo)
		for _, v := range modInfos {
			modInfosMap[v.ID] = v
		}

		// TODO: multithreading????

		modFileInfosMap := make(map[uint32]modFileInfo)
		referencedModPaths := make([]string, 0, len(modsList))
		successes := 0
		remainingFileIDs := make([]uint32, 0, len(modsList))

		// 1st pass: query mod metadata for every CurseForge file
		for _, v := range modsList {
			modInfoValue, ok := modInfosMap[v.ProjectID]
			if !ok {
				fmt.Printf("Failed to obtain information for project/file IDs %d/%d\n", v.ProjectID, v.FileID)
				continue
			}

			found := false
			var fileInfo modFileInfo
			for _, fileInfo = range modInfoValue.LatestFiles {
				if fileInfo.ID == v.FileID {
					found = true
					break
				}
			}
			if found {
				modFileInfosMap[v.FileID] = fileInfo
			} else {
				remainingFileIDs = append(remainingFileIDs, v.FileID)
			}
		}

		// 2nd pass: query files that weren't in the previous results
		fmt.Println("Querying Curse API for file info...")

		modFileInfos, err := cfDefaultClient.getFileInfoMultiple(remainingFileIDs)
		if err != nil {
			return fmt.Errorf("failed to obtain project file information: %w", err)
		}

		for _, v := range modFileInfos {
			modFileInfosMap[v.ID] = v
		}

		// 3rd pass: create mod files for every file
		for _, v := range modsList {
			modInfoValue, ok := modInfosMap[v.ProjectID]
			if !ok {
				fmt.Printf("Failed to obtain project information for project/file IDs %d/%d\n", v.ProjectID, v.FileID)
				continue
			}

			modFileInfoValue, ok := modFileInfosMap[v.FileID]
			if !ok {
				fmt.Printf("Failed to obtain project file information for project/file IDs %d/%d\n", v.ProjectID, v.FileID)
				continue
			}

			err = createModFile(modInfoValue, modFileInfoValue, &index, v.OptionalDisabled)
			if err != nil {
				return fmt.Errorf("failed to save project %q: %w", modInfoValue.Name, err)
			}

			modFilePath := getPathForFile(modInfoValue.GameID, modInfoValue.ClassID, modInfoValue.PrimaryCategoryID, modInfoValue.Slug)
			ref, err := filepath.Abs(filepath.Join(filepath.Dir(modFilePath), modFileInfoValue.FileName))
			if err == nil {
				referencedModPaths = append(referencedModPaths, ref)
			}

			fmt.Printf("Imported dependency \"%s\" successfully!\n", modInfoValue.Name)
			successes++
		}

		fmt.Printf("Successfully imported %d/%d dependencies!\n", successes, len(modsList))

		fmt.Println("Reading override files...")
		filesList, err := packImport.GetFiles()
		if err != nil {
			return fmt.Errorf("failed to read override files: %w", err)
		}

		successes = 0
		for _, v := range filesList {
			filePath := index.ResolveIndexPath(v.Name())
			filePathAbs, err := filepath.Abs(filePath)
			if err == nil {
				found := slices.Contains(referencedModPaths, filePathAbs)
				if found {
					fmt.Printf("Ignored file \"%s\" (referenced by metadata)\n", filePath)
					successes++
					continue
				}
				if v.Name() == "manifest.json" || v.Name() == "minecraftinstance.json" || v.Name() == ".curseclient" {
					fmt.Printf("Ignored file \"%s\"\n", v.Name())
					successes++
					continue
				}
			}

			f, err := os.Create(filePath)
			if err != nil {
				// Attempt to create the containing directory
				err2 := os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
				if err2 == nil {
					f, err = os.Create(filePath)
				}
				if err != nil {
					if err2 != nil {
						return fmt.Errorf("failed to create directories for %q: %w", filePath, err2)
					}
					return fmt.Errorf("failed to write file %q: %w", filePath, err)
				}
			}
			src, err := v.Open()
			if err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to read file %q: %w", filePath, err)
			}
			_, err = io.Copy(f, src)
			if err != nil {
				_ = f.Close()
				_ = src.Close()
				return fmt.Errorf("failed to copy file %q: %w", filePath, err)
			}

			fmt.Printf("Copied file \"%s\" successfully!\n", filePath)
			if err := f.Close(); err != nil {
				_ = src.Close()
				return fmt.Errorf("failed to close file %q: %w", filePath, err)
			}
			if err := src.Close(); err != nil {
				return fmt.Errorf("failed to close source file %q: %w", filePath, err)
			}
			successes++
		}
		if len(filesList) > 0 {
			fmt.Printf("Successfully copied %d/%d files!\n", successes, len(filesList))
			err = index.Refresh()
			if err != nil {
				return fmt.Errorf("failed to refresh index: %w", err)
			}
		} else {
			fmt.Println("No files copied!")
		}

		err = index.Write()
		if err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
		err = pack.UpdateIndexHash()
		if err != nil {
			return fmt.Errorf("failed to update index hash: %w", err)
		}
		err = pack.Write()
		if err != nil {
			return fmt.Errorf("failed to write pack: %w", err)
		}
		return nil
	},
}

func init() {
	curseforgeCmd.AddCommand(importCmd)
}
