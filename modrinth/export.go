package modrinth

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"sort"
	"strconv"

	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/spf13/viper"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the current modpack into a .mrpack for Modrinth",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Loading modpack...")
		pack, err := core.LoadPack()
		if err != nil {
			return fmt.Errorf("failed to load modpack: %w", err)
		}
		index, err := pack.LoadIndex()
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}
		// Do a refresh to ensure files are up to date
		err = index.Refresh()
		if err != nil {
			return fmt.Errorf("failed to refresh index: %w", err)
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
			return fmt.Errorf("failed to write modpack: %w", err)
		}

		fmt.Println("Reading external files...")
		mods, err := index.LoadAllMods()
		if err != nil {
			return fmt.Errorf("error reading mod metadata: %w", err)
		}

		allowOverrides := viper.GetBool("modrinth.export.allow-overrides")
		if err := core.ValidatePlatformReferences(mods, "modrinth", allowOverrides); err != nil {
			return err
		}
		for i, mod := range mods {
			if err := validatePackEnvironment(getModrinthEnvironment(mod)); err != nil {
				return fmt.Errorf("invalid environment for %s: %w", mod.Name, err)
			}
			mods[i], err = platformExportMod(mod)
			if err != nil {
				return fmt.Errorf("resolve Modrinth file for %s: %w", mod.Name, err)
			}
		}

		exportPath, err := pack.WriteExportFile(viper.GetString("modrinth.export.output"), ".mrpack", func(writer io.Writer) error {
			exp := zip.NewWriter(writer)
			defer exp.Close()

			// Add an overrides folder even if there are no files to go in it
			_, err = exp.Create("overrides/")
			if err != nil {
				return fmt.Errorf("failed to add overrides folder: %w", err)
			}

			fmt.Printf("Retrieving %v external files...\n", len(mods))

			restrictDomains := viper.GetBool("modrinth.export.restrictDomains")

			for _, mod := range mods {
				if !canBeIncludedDirectly(mod, restrictDomains) {
					cmdshared.PrintDisclaimer(false)
					break
				}
			}

			session, err := core.CreateDownloadSession(mods, []string{"sha1", "sha512", "length-bytes"})
			if err != nil {
				return fmt.Errorf("error retrieving external files: %w", err)
			}

			if err := cmdshared.ListManualDownloads(session); err != nil {
				return err
			}

			manifestFiles := make([]PackFile, 0)
			for dl := range session.StartDownloads() {
				env := getModrinthEnvironment(dl.Mod)
				if err := validatePackEnvironment(env); err != nil {
					return fmt.Errorf("invalid environment for %s: %w", dl.Mod.Name, err)
				}
				if canBeIncludedDirectly(dl.Mod, restrictDomains) {
					if dl.Error != nil {
						return fmt.Errorf("download of %s (%s) failed: %w", dl.Mod.Name, dl.Mod.FileName, dl.Error)
					}
					for _, warning := range dl.Warnings {
						fmt.Printf("Warning for %s (%s): %v\n", dl.Mod.Name, dl.Mod.FileName, warning)
					}

					path, err := index.RelIndexPath(dl.Mod.GetDestFilePath())
					if err != nil {
						return fmt.Errorf("error resolving external file: %w", err)
					}

					hashes := make(map[string]string)
					hashes["sha1"] = dl.Hashes["sha1"]
					hashes["sha512"] = dl.Hashes["sha512"]
					fileSize, err := strconv.ParseUint(dl.Hashes["length-bytes"], 10, 64)
					if err != nil {
						return fmt.Errorf("invalid download size for %s: %w", dl.Mod.Name, err)
					}

					// Modrinth URLs must be RFC3986
					u, err := core.ReencodeURL(dl.Mod.Download.URL)
					if err != nil {
						return fmt.Errorf("error re-encoding download URL: %w", err)
					}

					manifestFiles = append(manifestFiles, PackFile{
						Path:      path,
						Hashes:    hashes,
						Env:       env,
						Downloads: []string{u},
						FileSize:  uint32(fileSize),
					})

					if err := dl.File.Close(); err != nil {
						return err
					}
					fmt.Printf("%s (%s) added to manifest\n", dl.Mod.Name, dl.Mod.FileName)
				} else {
					if dl.Mod.Environment != nil && (env.Client == "optional" || env.Server == "optional" || (env.Client == "unsupported" && env.Server == "unsupported")) {
						return fmt.Errorf("cannot bundle %s into overrides without losing its Modrinth environment; allow its download domain with --restrictDomains=false", dl.Mod.Name)
					}
					zipDir := modrinthOverrideDir(dl.Mod)
					if err := cmdshared.AddToZip(dl, exp, zipDir, &index); err != nil {
						return err
					}
				}
			}
			// sort by `path` property before serialising to ensure reproducibility
			sort.Slice(manifestFiles, func(i, j int) bool {
				return manifestFiles[i].Path < manifestFiles[j].Path
			})

			err = session.SaveIndex()
			if err != nil {
				return fmt.Errorf("error saving cache index: %w", err)
			}

			dependencies := make(map[string]string)
			dependencies["minecraft"], err = pack.GetMCVersion()
			if err != nil {
				return fmt.Errorf("error creating manifest: %w", err)
			}
			if quiltVersion, ok := pack.Versions["quilt"]; ok {
				dependencies["quilt-loader"] = quiltVersion
			} else if fabricVersion, ok := pack.Versions["fabric"]; ok {
				dependencies["fabric-loader"] = fabricVersion
			} else if forgeVersion, ok := pack.Versions["forge"]; ok {
				dependencies["forge"] = forgeVersion
			} else if neoforgeVersion, ok := pack.Versions["neoforge"]; ok {
				dependencies["neoforge"] = neoforgeVersion
			}

			manifest := Pack{
				FormatVersion: 1,
				Game:          "minecraft",
				VersionID:     pack.Version,
				Name:          pack.Name,
				Summary:       pack.Description,
				Files:         manifestFiles,
				Dependencies:  dependencies,
			}

			if len(pack.Version) == 0 {
				fmt.Println("Warning: pack.toml version field must not be empty to create a valid Modrinth pack")
			}

			manifestFile, err := exp.Create("modrinth.index.json")
			if err != nil {
				return fmt.Errorf("error creating manifest: %w", err)
			}

			w := json.NewEncoder(manifestFile)
			w.SetIndent("", "    ") // Documentation uses 4 spaces
			err = w.Encode(manifest)
			if err != nil {
				return fmt.Errorf("error writing manifest: %w", err)
			}

			if err := cmdshared.AddModrinthOverrides(&index, exp); err != nil {
				return err
			}

			err = exp.Close()
			if err != nil {
				return fmt.Errorf("error writing export file: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Println("Modpack exported to " + exportPath)
		return nil
	},
}

func getModrinthEnvironment(mod *core.Mod) *PackEnvironment {
	if mod.Environment != nil {
		return &PackEnvironment{Client: mod.Environment.Client, Server: mod.Environment.Server}
	}

	installed := "required"
	if mod.Option != nil && mod.Option.Optional {
		installed = "optional"
	}
	client, server := installed, installed
	switch mod.Side {
	case core.ClientSide:
		server = "unsupported"
	case core.ServerSide:
		client = "unsupported"
	}
	return &PackEnvironment{Client: client, Server: server}
}

func modrinthOverrideDir(mod *core.Mod) string {
	env := getModrinthEnvironment(mod)
	if env.Client == "unsupported" && env.Server != "unsupported" {
		return "server-overrides"
	}
	if env.Server == "unsupported" && env.Client != "unsupported" {
		return "client-overrides"
	}
	return "overrides"
}

var whitelistedHosts = []string{
	"cdn.modrinth.com",
	"github.com",
	"raw.githubusercontent.com",
	"gitlab.com",
}

func canBeIncludedDirectly(mod *core.Mod, restrictDomains bool) bool {
	if mod.Download.Mode == core.ModeURL || mod.Download.Mode == "" {
		if !restrictDomains {
			return true
		}

		modUrl, err := url.Parse(mod.Download.URL)
		if err == nil {
			if slices.Contains(whitelistedHosts, modUrl.Host) {
				return true
			}
		}
	}
	return false
}

func init() {
	modrinthCmd.AddCommand(exportCmd)
	exportCmd.Flags().Bool("allow-overrides", false, "Allow files without Modrinth references (may be rejected)")
	_ = viper.BindPFlag("modrinth.export.allow-overrides", exportCmd.Flags().Lookup("allow-overrides"))
	exportCmd.Flags().Bool("restrictDomains", true, "Restricts domains to those allowed by modrinth.com")
	exportCmd.Flags().StringP("output", "o", "", "The file or directory to export the modpack to")
	_ = viper.BindPFlag("modrinth.export.restrictDomains", exportCmd.Flags().Lookup("restrictDomains"))
	_ = viper.BindPFlag("modrinth.export.output", exportCmd.Flags().Lookup("output"))
}
