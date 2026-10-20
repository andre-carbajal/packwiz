package cmd

import (
	"fmt"

	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// UpdateCmd represents the update command
var UpdateCmd = &cobra.Command{
	Use:     "update [name]",
	Short:   "Update an external file (or all external files) in the modpack",
	Aliases: []string{"upgrade"},
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: --check flag?
		// TODO: specify multiple files to update at once?

		fmt.Println("Loading modpack...")
		pack, err := core.LoadPack()
		if err != nil {
			return fmt.Errorf("failed to load modpack: %w", err)
		}
		index, err := pack.LoadIndex()
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		var singleUpdatedName string
		if viper.GetBool("update.all") {
			filesWithUpdater := make(map[string][]*core.Mod)
			fmt.Println("Reading metadata files...")
			mods, err := index.LoadAllMods()
			if err != nil {
				return fmt.Errorf("failed to update all files: %w", err)
			}
			for _, modData := range mods {
				if len(modData.Update) == 0 {
					fmt.Printf("A supported update system for %q cannot be found.\n", modData.Name)
					continue
				}
				source, err := modData.PrimaryUpdateSource()
				if err != nil {
					return err
				}
				filesWithUpdater[source] = append(filesWithUpdater[source], modData)
			}

			fmt.Println("Checking for updates...")
			updatesFound := false
			updatableFiles := make(map[string][]*core.Mod)
			updaterCachedStateMap := make(map[string][]any)
			for k, v := range filesWithUpdater {
				checks, err := core.Updaters[k].CheckUpdate(v, pack)
				if err != nil {
					// TODO: do we return err code 1?
					fmt.Printf("Failed to check updates for %s: %s\n", k, err.Error())
					continue
				}
				for i, check := range checks {
					if check.Error != nil {
						// TODO: do we return err code 1?
						fmt.Printf("Failed to check updates for %s: %s\n", v[i].Name, check.Error.Error())
						continue
					}
					if check.UpdateAvailable {
						if v[i].Pin {
							fmt.Printf("Update skipped for pinned mod %s\n", v[i].Name)
							continue
						}

						if !updatesFound {
							fmt.Println("Updates found:")
							updatesFound = true
						}
						fmt.Printf("%s: %s\n", v[i].Name, check.UpdateString)
						updatableFiles[k] = append(updatableFiles[k], v[i])
						updaterCachedStateMap[k] = append(updaterCachedStateMap[k], check.CachedState)
					}
				}
			}

			if !updatesFound {
				fmt.Println("All files are up to date!")
				return nil
			}

			proceed, err := cmdshared.PromptYesNo("Do you want to update? [Y/n]: ")
			if err != nil {
				return err
			}
			if !proceed {
				fmt.Println("Cancelled!")
				return nil
			}

			for k, v := range updatableFiles {
				err := core.Updaters[k].DoUpdate(v, updaterCachedStateMap[k])
				if err != nil {
					// TODO: do we return err code 1?
					fmt.Println(err.Error())
					continue
				}
				for _, modData := range v {
					format, hash, err := modData.Write()
					if err != nil {
						fmt.Println(err.Error())
						continue
					}
					err = index.RefreshFileWithHash(modData.GetFilePath(), format, hash, true)
					if err != nil {
						fmt.Println(err.Error())
						continue
					}
				}
			}
		} else {
			if len(args) < 1 || len(args[0]) == 0 {
				return fmt.Errorf("must specify a valid file, or use the --all flag")
			}
			modPath, ok := index.FindMod(args[0])
			if !ok {
				return fmt.Errorf("can't find this file; run packwiz refresh and use the name of the .pw.toml file (defaults to the project slug)")
			}
			modData, err := core.LoadMod(modPath)
			if err != nil {
				return fmt.Errorf("failed to load mod metadata: %w", err)
			}
			if modData.Pin {
				return fmt.Errorf("version is pinned; run the unpin command to allow updating")
			}
			singleUpdatedName = modData.Name
			source, err := modData.PrimaryUpdateSource()
			if err != nil {
				return err
			}
			updater := core.Updaters[source]

			check, err := updater.CheckUpdate([]*core.Mod{&modData}, pack)
			if err != nil {
				return fmt.Errorf("failed to check for updates: %w", err)
			}
			if len(check) != 1 {
				return fmt.Errorf("invalid update check response")
			}

			if check[0].UpdateAvailable {
				fmt.Printf("Update available: %s\n", check[0].UpdateString)

				err = updater.DoUpdate([]*core.Mod{&modData}, []any{check[0].CachedState})
				if err != nil {
					return fmt.Errorf("failed to update mod: %w", err)
				}

				format, hash, err := modData.Write()
				if err != nil {
					return fmt.Errorf("failed to write mod metadata: %w", err)
				}
				err = index.RefreshFileWithHash(modPath, format, hash, true)
				if err != nil {
					return err
				}
			} else {
				fmt.Printf("\"%s\" is already up to date!\n", modData.Name)
				return nil
			}

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
		if viper.GetBool("update.all") {
			fmt.Println("Files updated!")
		} else {
			fmt.Printf("\"%s\" updated!\n", singleUpdatedName)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(UpdateCmd)

	UpdateCmd.Flags().BoolP("all", "a", false, "Update all external files")
	_ = viper.BindPFlag("update.all", UpdateCmd.Flags().Lookup("all"))
}
