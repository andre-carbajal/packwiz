package url

import (
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var installCmd = &cobra.Command{
	Use:     "add [name] [url]",
	Short:   "Add an external file from a direct download link, for sites that are not directly supported by packwiz",
	Aliases: []string{"install", "get"},
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pack, err := core.LoadPack()
		if err != nil {
			return fmt.Errorf("failed to load modpack: %w", err)
		}

		dl, err := url.Parse(args[1])
		if err != nil {
			return fmt.Errorf("failed to parse URL: %w", err)
		}
		if dl.Scheme != "https" && dl.Scheme != "http" {
			return fmt.Errorf("unsupported URL scheme: %s", dl.Scheme)
		}

		// TODO: consider using colors for these warnings but those can have issues on windows
		force, err := cmd.Flags().GetBool("force")
		if err != nil {
			return err
		}
		if !force {
			var msg string
			// TODO: update when github command is added
			// TODO: make this generic?
			//if dl.Host == "www.github.com" || dl.Host == "github.com" {
			//	msg = "github add " + args[1]
			//}
			if strings.HasSuffix(dl.Host, "modrinth.com") {
				msg = "modrinth add " + args[1]
			}
			if strings.HasSuffix(dl.Host, "curseforge.com") || strings.HasSuffix(dl.Host, "forgecdn.net") {
				msg = "curseforge add " + args[1]
			}
			if msg != "" {
				return fmt.Errorf("consider using packwiz %s instead; use --force to add this file without update metadata", msg)
			}
		}

		hash, err := getHash(args[1])
		if err != nil {
			return fmt.Errorf("failed to retrieve SHA256 hash for file: %w", err)
		}

		index, err := pack.LoadIndex()
		if err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}

		filename := path.Base(dl.Path)
		modMeta := core.Mod{
			Name:     args[0],
			FileName: filename,
			Side:     core.UniversalSide,
			Download: core.ModDownload{
				URL:        args[1],
				HashFormat: "sha256",
				Hash:       hash,
			},
		}

		folder := viper.GetString("meta-folder")
		if folder == "" {
			folder = "mods"
		}
		destPathName, err := cmd.Flags().GetString("meta-name")
		if err != nil {
			return err
		}
		if destPathName == "" {
			destPathName = core.SlugifyName(args[0])
		}
		destPath := modMeta.SetMetaPath(filepath.Join(viper.GetString("meta-folder-base"), folder,
			destPathName+core.MetaExtension))

		format, hash, err := modMeta.Write()
		if err != nil {
			return fmt.Errorf("failed to write mod metadata: %w", err)
		}
		err = index.RefreshFileWithHash(destPath, format, hash, true)
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
		fmt.Printf("Successfully added %s (%s) from: %s\n", args[0], destPath, args[1])
		return nil
	},
}

func getHash(url string) (string, error) {
	mainHasher, err := core.GetHashImpl("sha256")
	if err != nil {
		return "", err
	}
	resp, err := core.GetWithUA(url, "application/octet-stream")
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to download: unexpected response status: %v", resp.Status)
	}

	_, err = io.Copy(mainHasher, resp.Body)
	if err != nil {
		return "", err
	}

	return mainHasher.HashToString(mainHasher.Sum(nil)), nil
}

func init() {
	urlCmd.AddCommand(installCmd)

	installCmd.Flags().Bool("force", false, "Add a file even if the download URL is supported by packwiz in an alternative command (which may support dependencies and updates)")
	installCmd.Flags().String("meta-name", "", "Filename to use for the created metadata file (defaults to a name generated from the name you supply)")
}
