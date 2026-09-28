package cmd

import (
	"fmt"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

func pinMod(args []string, pinned bool) error {
	fmt.Println("Loading modpack...")
	pack, err := core.LoadPack()
	if err != nil {
		return fmt.Errorf("failed to load modpack: %w", err)
	}
	index, err := pack.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}
	modPath, ok := index.FindMod(args[0])
	if !ok {
		return fmt.Errorf("can't find this file; run packwiz refresh and use the name of the .pw.toml file (defaults to the project slug)")
	}
	modData, err := core.LoadMod(modPath)
	if err != nil {
		return fmt.Errorf("failed to load mod metadata: %w", err)
	}
	modData.Pin = pinned
	format, hash, err := modData.Write()
	if err != nil {
		return fmt.Errorf("failed to write mod metadata: %w", err)
	}
	err = index.RefreshFileWithHash(modPath, format, hash, true)
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

	message := "pinned"
	if !pinned {
		message = "unpinned"
	}
	fmt.Printf("%s %s successfully!\n", args[0], message)
	return nil
}

// pinCmd represents the pin command
var pinCmd = &cobra.Command{
	Use:     "pin",
	Short:   "Pin a file so it does not get updated automatically",
	Aliases: []string{"hold"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return pinMod(args, true)
	},
}

// unpinCmd represents the unpin command
var unpinCmd = &cobra.Command{
	Use:     "unpin",
	Short:   "Unpin a file so it receives updates",
	Aliases: []string{"unhold"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return pinMod(args, false)
	},
}

func init() {
	rootCmd.AddCommand(pinCmd)
	rootCmd.AddCommand(unpinCmd)
}
