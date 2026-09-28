package migrate

import (
	"fmt"
	"os"

	packCmd "github.com/andre-carbajal/packwiz/cmd"
	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var minecraftCommand = &cobra.Command{
	Use:     "minecraft [version]",
	Short:   "Migrate your Minecraft version to a newer version.",
	Aliases: []string{"mc"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modpack, err := core.LoadPack()
		if err != nil {
			// Check if it's a no such file or directory error
			if os.IsNotExist(err) {
				return fmt.Errorf("no pack.toml file found, run 'packwiz init' to create one: %w", err)
			}
			return fmt.Errorf("error loading pack: %w", err)
		}
		currentVersion, err := modpack.GetMCVersion()
		if err != nil {
			return fmt.Errorf("error getting Minecraft version from pack: %w", err)
		}
		wantedMCVersion := args[0]
		if wantedMCVersion == currentVersion {
			fmt.Printf("Minecraft version is already %s!\n", wantedMCVersion)
			return nil
		}
		mcVersions, err := cmdshared.GetValidMCVersions()
		if err != nil {
			return fmt.Errorf("error getting Minecraft versions: %w", err)
		}
		if err := mcVersions.CheckValid(wantedMCVersion); err != nil {
			return err
		}
		// Set the version in the pack
		modpack.Versions["minecraft"] = wantedMCVersion
		// Write the pack to disk
		err = modpack.Write()
		if err != nil {
			return fmt.Errorf("error writing pack.toml: %w", err)
		}
		fmt.Printf("Successfully updated Minecraft version to %s\n", wantedMCVersion)
		// Prompt the user if they want to update the loader too while they're at it.
		updateLoader, err := cmdshared.PromptYesNo("Would you like to update your loader version to the latest version for this Minecraft version? [Y/n] ")
		if err != nil {
			return err
		}
		if updateLoader {
			// We'll run the loader command to update to latest
			if err := loaderCommand.RunE(loaderCommand, []string{"latest"}); err != nil {
				return err
			}
		}
		// Prompt the user to update their mods too.
		updateMods, err := cmdshared.PromptYesNo("Would you like to update your mods to the latest versions for this Minecraft version? [Y/n] ")
		if err != nil {
			return err
		}
		if updateMods {
			// Run the update command
			viper.Set("update.all", true)
			if err := packCmd.UpdateCmd.RunE(packCmd.UpdateCmd, []string{}); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(minecraftCommand)
}
