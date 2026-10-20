package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:          "link [mod]",
	Short:        "Link the identical installed file to CurseForge and Modrinth",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		cfURL, _ := cmd.Flags().GetString("curseforge")
		mrURL, _ := cmd.Flags().GetString("modrinth")
		if all == (len(args) == 1) {
			return fmt.Errorf("specify a mod or --all, not both")
		}
		if cfURL != "" && mrURL != "" {
			return fmt.Errorf("specify only one platform URL")
		}
		if all && (cfURL != "" || mrURL != "") {
			return fmt.Errorf("platform URLs require a single mod")
		}
		pack, err := core.LoadPack()
		if err != nil {
			return err
		}
		index, err := pack.LoadIndex()
		if err != nil {
			return err
		}
		var mods []*core.Mod
		if all {
			mods, err = index.LoadAllMods()
		} else {
			path, ok := index.FindMod(args[0])
			if !ok {
				return fmt.Errorf("cannot find mod %q", args[0])
			}
			var mod core.Mod
			mod, err = core.LoadMod(path)
			mods = []*core.Mod{&mod}
		}
		if err != nil {
			return err
		}
		var failures []error
		skippedMods := make(map[string][]string)
		linkedCount, alreadyLinkedCount := 0, 0
		authFailed := make(map[string]string)
		for _, mod := range mods {
			source, err := mod.PrimaryUpdateSource()
			if err != nil {
				failures = append(failures, err)
				continue
			}
			target, reference := "", ""
			switch {
			case cfURL != "":
				target, reference = "curseforge", cfURL
			case mrURL != "":
				target, reference = "modrinth", mrURL
			case source == "curseforge":
				target = "modrinth"
			case source == "modrinth":
				target = "curseforge"
			default:
				failures = append(failures, fmt.Errorf("%s has no CurseForge/Modrinth origin", mod.Name))
				continue
			}
			if target == source {
				failures = append(failures, fmt.Errorf("%s is already managed by %s; link the other platform", mod.Name, source))
				continue
			}
			if _, exists := mod.Update[target]; exists && reference == "" {
				alreadyLinkedCount++
				continue
			}
			if authFailed[target] != "" {
				skippedMods[target] = append(skippedMods[target], mod.Name)
				continue
			}
			if err := core.LinkMod(mod, target, reference); err != nil {
				var authErr *core.PlatformAuthError
				if errors.As(err, &authErr) {
					authFailed[target] = authErr.Platform
					failures = append(failures, fmt.Errorf("%s: %w; check CURSEFORGE_API_KEY and retry", mod.Name, authErr))
					continue
				}
				failures = append(failures, fmt.Errorf("%s: %w", mod.Name, err))
				continue
			}
			mod.UpdateSource = source
			format, hash, err := mod.Write()
			if err != nil {
				return err
			}
			if err := index.RefreshFileWithHash(mod.GetFilePath(), format, hash, true); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Linked %s to %s\n", mod.Name, target)
			linkedCount++
		}
		if err := index.Write(); err != nil {
			return err
		}
		if err := pack.UpdateIndexHash(); err != nil {
			return err
		}
		if err := pack.Write(); err != nil {
			return err
		}
		if !all {
			return errors.Join(failures...)
		}

		skippedCount := len(skippedMods["curseforge"]) + len(skippedMods["modrinth"])
		summary := fmt.Sprintf("Link summary: %d linked successfully, %d already linked, %d failed, %d skipped.", linkedCount, alreadyLinkedCount, len(failures), skippedCount)
		if len(failures) == 0 && skippedCount == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), summary)
			return nil
		}

		details := []error{errors.New(summary)}
		if linkedCount > 0 {
			details = append(details, errors.New("Successful links were saved; failed or skipped mods were left unchanged."))
		}
		for _, target := range []string{"curseforge", "modrinth"} {
			if skipped := skippedMods[target]; len(skipped) > 0 {
				details = append(details, fmt.Errorf("Not attempted after %s authentication failed: %s", authFailed[target], strings.Join(skipped, ", ")))
			}
		}
		details = append(details, failures...)
		return errors.Join(details...)
	},
}

func init() {
	rootCmd.AddCommand(linkCmd)
	linkCmd.Flags().BoolP("all", "a", false, "Complete missing references for all mods")
	linkCmd.Flags().String("curseforge", "", "CurseForge project or file URL (must contain identical bytes)")
	linkCmd.Flags().String("modrinth", "", "Modrinth project, version or file URL (must contain identical bytes)")
}
