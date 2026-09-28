package cmd

import (
	"fmt"
	"os"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:     "remove",
	Short:   "Remove an external file from the modpack; equivalent to manually removing the file and running packwiz refresh",
	Aliases: []string{"delete", "uninstall", "rm"},
	Args:    cobra.ExactArgs(1),
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
		resolvedMod, ok := index.FindMod(args[0])
		if !ok {
			return fmt.Errorf("can't find this file; run packwiz refresh and use the name of the .pw.toml file (defaults to the project slug)")
		}
		err = os.Remove(resolvedMod)
		if err != nil {
			return fmt.Errorf("failed to remove file: %w", err)
		}
		fmt.Println("Removing file from index...")
		err = index.RemoveFile(resolvedMod)
		if err != nil {
			return err
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

		fmt.Printf("%s removed successfully!\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
