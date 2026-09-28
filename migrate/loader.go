package migrate

import (
	"fmt"
	"os"
	"slices"

	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/andre-carbajal/packwiz/core"
	"github.com/spf13/cobra"
)

var loaderCommand = &cobra.Command{
	Use:   "loader [version|latest|recommended]",
	Short: "Migrate your modloader version to a newer version.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modpack, err := core.LoadPack()
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("no pack.toml file found, run 'packwiz init' to create one: %w", err)
			}
			return fmt.Errorf("error loading pack: %w", err)
		}
		currentLoaders := modpack.GetLoaders()
		if len(currentLoaders) == 0 {
			return fmt.Errorf("no loader is currently set in your pack.toml")
		}
		if len(currentLoaders) > 1 {
			return fmt.Errorf("multiple loaders in pack.toml are not supported")
		}
		mcVersion, err := modpack.GetMCVersion()
		if err != nil {
			return fmt.Errorf("error getting Minecraft version: %w", err)
		}

		if args[0] == "latest" || args[0] == "recommended" {
			fmt.Printf("Updating to %s loader version\n", args[0])
			queryType := core.Latest
			if args[0] == "recommended" {
				queryType = core.Recommended
			}
			for _, loader := range currentLoaders {
				versionData, gottenLoader, err := getVersionsForLoader(loader, mcVersion, queryType)
				if err != nil {
					return err
				}
				if !updatePackToVersion(versionData.Latest, modpack, gottenLoader) {
					continue
				}
				if err := modpack.Write(); err != nil {
					return fmt.Errorf("error writing pack.toml: %w", err)
				}
			}
			return nil
		}

		fmt.Println("Updating to explicit loader version")
		versionData, loader, err := getVersionsForLoader(currentLoaders[0], mcVersion, core.Latest)
		if err != nil {
			return err
		}
		if loader.Name == "liteloader" {
			fmt.Println("LiteLoader only has 1 version per Minecraft version so we're unable to update!")
			return nil
		}
		wantedVersion := args[0]
		if loader.Name == "forge" || loader.Name == "neoforge" {
			wantedVersion = cmdshared.GetRawForgeVersion(wantedVersion)
		}
		if err := validateVersion(versionData.Versions, wantedVersion, loader); err != nil {
			return err
		}
		if !updatePackToVersion(wantedVersion, modpack, loader) && loader.Name != "forge" && loader.Name != "neoforge" {
			return fmt.Errorf("%s is already on version %s", loader.FriendlyName, wantedVersion)
		}
		if err := modpack.Write(); err != nil {
			return fmt.Errorf("error writing pack.toml: %w", err)
		}
		return nil
	},
}

func init() {
	migrateCmd.AddCommand(loaderCommand)
}

func getVersionsForLoader(loader, mcVersion string, queryType core.QueryType) (*core.ModLoaderVersions, core.ModLoaderComponent, error) {
	gottenLoader, ok := core.ModLoaders[loader]
	if !ok {
		return nil, core.ModLoaderComponent{}, fmt.Errorf("unknown loader %s", loader)
	}
	versionData, err := core.DoQuery(core.MakeQuery(gottenLoader, mcVersion).WithQueryType(queryType))
	if err != nil {
		return nil, core.ModLoaderComponent{}, fmt.Errorf("error getting version list for %s: %w", gottenLoader.FriendlyName, err)
	}
	return versionData, gottenLoader, nil
}

func validateVersion(versions []string, version string, gottenLoader core.ModLoaderComponent) error {
	if !slices.Contains(versions, version) {
		return fmt.Errorf("version %s is not valid for %s", version, gottenLoader.FriendlyName)
	}
	return nil
}

func updatePackToVersion(version string, modpack core.Pack, loader core.ModLoaderComponent) bool {
	if version == modpack.Versions[loader.Name] {
		fmt.Printf("%s is already on version %s!\n", loader.FriendlyName, version)
		return false
	}
	modpack.Versions[loader.Name] = version
	fmt.Printf("Updated %s to version %s\n", loader.FriendlyName, version)
	return true
}
