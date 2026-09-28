package cmd

import (
	"fmt"

	"github.com/andre-carbajal/packwiz/cmdshared"

	"slices"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

// rehashCmd represents the rehash command
var rehashCmd = &cobra.Command{
	Use:   "rehash [hash format]",
	Short: "Migrate all hashes to a specific format",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		// Load pack
		pack, err := core.LoadPack()
		if err != nil {
			return fmt.Errorf("failed to load modpack: %w", err)
		}

		// Load index
		index, err := pack.LoadIndex()
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		// Load mods
		mods, err := index.LoadAllMods()
		if err != nil {
			return fmt.Errorf("failed to load mod metadata: %w", err)
		}

		if !slices.Contains([]string{"sha1", "sha512", "sha256"}, args[0]) {
			return fmt.Errorf("hash format %q is not supported", args[0])
		}

		session, err := core.CreateDownloadSession(mods, []string{args[0]})
		if err != nil {
			return fmt.Errorf("error retrieving external files: %w", err)
		}

		if err := cmdshared.ListManualDownloads(session); err != nil {
			return err
		}

		for dl := range session.StartDownloads() {
			if dl.Error != nil {
				fmt.Printf("Error retrieving %s: %v\n", dl.Mod.Name, dl.Error)
			} else {
				dl.Mod.Download.HashFormat = args[0]
				dl.Mod.Download.Hash = dl.Hashes[args[0]]
				_, _, err := dl.Mod.Write()
				if err != nil {
					return fmt.Errorf("error saving mod %s: %w", dl.Mod.Name, err)
				}
			}
			// TODO pass the hash to index instead of recomputing from scratch
		}

		err = session.SaveIndex()
		if err != nil {
			return fmt.Errorf("error saving cache index: %w", err)
		}

		err = index.Refresh()
		if err != nil {
			return fmt.Errorf("error refreshing index: %w", err)
		}

		err = index.Write()
		if err != nil {
			return fmt.Errorf("error writing index: %w", err)
		}

		err = pack.UpdateIndexHash()
		if err != nil {
			return fmt.Errorf("error updating index hash: %w", err)
		}

		err = pack.Write()
		if err != nil {
			return fmt.Errorf("error writing pack: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rehashCmd)
}
