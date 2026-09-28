package cmd

import (
	"fmt"

	"github.com/spf13/viper"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

// refreshCmd represents the refresh command
var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh the index file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Loading modpack...")
		pack, err := core.LoadPack()
		if err != nil {
			return fmt.Errorf("failed to load modpack: %w", err)
		}
		build, err := cmd.Flags().GetBool("build")
		if err != nil {
			return err
		}
		if build {
			viper.Set("no-internal-hashes", false)
		} else if viper.GetBool("no-internal-hashes") {
			fmt.Println("Note: no-internal-hashes mode is set, no hashes will be saved. Use --build to override this for distribution.")
		}
		index, err := pack.LoadIndex()
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}
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
			return fmt.Errorf("failed to write pack: %w", err)
		}
		fmt.Println("Index refreshed!")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(refreshCmd)

	refreshCmd.Flags().Bool("build", false, "Only has an effect in no-internal-hashes mode: generates internal hashes for distribution with packwiz-installer")
}
