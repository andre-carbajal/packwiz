package curseforge

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"strconv"

	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/andre-carbajal/packwiz/core"
	"github.com/andre-carbajal/packwiz/curseforge/packinterop"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the current modpack into a .zip for curseforge",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		side := viper.GetString("curseforge.export.side")
		if side != core.UniversalSide && side != core.ServerSide && side != core.ClientSide {
			return fmt.Errorf("invalid side %q, must be one of client, server, or both", side)
		}

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
		i := 0
		// Filter mods by side
		// TODO: opt-in optional disabled filtering?
		for _, mod := range mods {
			if mod.Side == side || mod.Side == core.EmptySide || mod.Side == core.UniversalSide || side == core.UniversalSide {
				mods[i] = mod
				i++
			}
		}
		mods = mods[:i]

		allowOverrides := viper.GetBool("curseforge.export.allow-overrides")
		if err := core.ValidatePlatformReferences(mods, "curseforge", allowOverrides); err != nil {
			return err
		}

		var exportData cfExportData
		exportDataUnparsed, ok := pack.Export["curseforge"]
		if ok {
			exportData, err = parseExportData(exportDataUnparsed)
			if err != nil {
				return fmt.Errorf("failed to parse export metadata: %w", err)
			}
		}

		exportPath, err := pack.WriteExportFile(viper.GetString("curseforge.export.output"), ".zip", func(writer io.Writer) error {
			exp := zip.NewWriter(writer)
			defer exp.Close()

			// Add an overrides folder even if there are no files to go in it
			_, err = exp.Create("overrides/")
			if err != nil {
				return fmt.Errorf("failed to add overrides folder: %w", err)
			}

			cfFileRefs := make([]packinterop.AddonFileReference, 0, len(mods))
			nonCfMods := make([]*core.Mod, 0)
			for _, mod := range mods {
				projectRaw, ok := mod.GetParsedUpdateData("curseforge")
				// If the mod has curseforge metadata, add it to cfFileRefs
				if ok {
					p := projectRaw.(cfUpdateData)
					cfFileRefs = append(cfFileRefs, packinterop.AddonFileReference{
						ProjectID:        p.ProjectID,
						FileID:           p.FileID,
						OptionalDisabled: mod.Option != nil && mod.Option.Optional && !mod.Option.Default,
					})
				} else {
					nonCfMods = append(nonCfMods, mod)
				}
			}

			// Download external files and save directly into the zip
			if len(nonCfMods) > 0 {
				fmt.Printf("Retrieving %v external files to store in the modpack zip...\n", len(nonCfMods))
				cmdshared.PrintDisclaimer(true)

				session, err := core.CreateDownloadSession(nonCfMods, []string{})
				if err != nil {
					return fmt.Errorf("error retrieving external files: %w", err)
				}

				if err := cmdshared.ListManualDownloads(session); err != nil {
					return err
				}

				for dl := range session.StartDownloads() {
					if err := cmdshared.AddToZip(dl, exp, "overrides", &index); err != nil {
						return err
					}
				}

				err = session.SaveIndex()
				if err != nil {
					return fmt.Errorf("error saving cache index: %w", err)
				}
			}

			manifestFile, err := exp.Create("manifest.json")
			if err != nil {
				return fmt.Errorf("error creating manifest: %w", err)
			}

			err = packinterop.WriteManifestFromPack(pack, cfFileRefs, exportData.ProjectID, manifestFile)
			if err != nil {
				return fmt.Errorf("error writing manifest: %w", err)
			}

			err = createModlist(exp, mods)
			if err != nil {
				return fmt.Errorf("error creating mod list: %w", err)
			}

			if err := cmdshared.AddNonMetafileOverrides(&index, exp); err != nil {
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

func createModlist(zw *zip.Writer, mods []*core.Mod) error {
	modlistFile, err := zw.Create("modlist.html")
	if err != nil {
		return err
	}

	w := bufio.NewWriter(modlistFile)

	_, err = w.WriteString("<ul>\r\n")
	if err != nil {
		return err
	}
	for _, mod := range mods {
		projectRaw, ok := mod.GetParsedUpdateData("curseforge")
		if !ok {
			// TODO: read homepage URL or something similar?
			// TODO: how to handle mods that don't have metadata???
			_, err = w.WriteString("<li>" + mod.Name + "</li>\r\n")
			if err != nil {
				return err
			}
			continue
		}
		project := projectRaw.(cfUpdateData)
		_, err = w.WriteString("<li><a href=\"https://www.curseforge.com/projects/" + strconv.FormatUint(uint64(project.ProjectID), 10) + "\">" + mod.Name + "</a></li>\r\n")
		if err != nil {
			return err
		}
	}
	_, err = w.WriteString("</ul>\r\n")
	if err != nil {
		return err
	}
	return w.Flush()
}

func init() {
	curseforgeCmd.AddCommand(exportCmd)
	exportCmd.Flags().Bool("allow-overrides", false, "Allow files without CurseForge references (may be rejected)")
	_ = viper.BindPFlag("curseforge.export.allow-overrides", exportCmd.Flags().Lookup("allow-overrides"))

	exportCmd.Flags().StringP("side", "s", "client", "The side to export mods with")
	_ = viper.BindPFlag("curseforge.export.side", exportCmd.Flags().Lookup("side"))
	exportCmd.Flags().StringP("output", "o", "", "The file or directory to export the modpack to")
	_ = viper.BindPFlag("curseforge.export.output", exportCmd.Flags().Lookup("output"))
}
