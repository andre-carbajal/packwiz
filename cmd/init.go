package cmd

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/andre-carbajal/packwiz/cmdshared"
	"github.com/andre-carbajal/packwiz/core"
	"github.com/fatih/camelcase"
	"github.com/igorsobreira/titlecase"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialise a packwiz modpack",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var existingPack *core.Pack
		_, err := os.Stat(viper.GetString("pack-file"))
		if err == nil && !viper.GetBool("init.reinit") {
			return fmt.Errorf("modpack metadata file already exists, use -r to override")
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("error checking pack file: %w", err)
		}
		if err == nil && viper.GetBool("init.reinit") {
			pack, err := core.LoadPack()
			if err != nil {
				return fmt.Errorf("error loading existing pack data: %w", err)
			}
			existingPack = &pack
		}

		name, err := cmd.Flags().GetString("name")
		if len(name) == 0 && existingPack != nil && !cmd.Flags().Changed("name") {
			name = existingPack.Name
		}
		if err != nil || len(name) == 0 {
			// Get current file directory name
			wd, err := os.Getwd()
			directoryName := "."
			if err == nil {
				directoryName = filepath.Base(wd)
			}
			if directoryName != "." && len(directoryName) > 0 {
				// Turn directory name into a space-seperated proper name
				name = titlecase.Title(strings.ReplaceAll(strings.ReplaceAll(strings.Join(camelcase.Split(directoryName), " "), " - ", " "), " _ ", " "))
				name, err = initReadValue("Modpack name ["+name+"]: ", name)
			} else {
				name, err = initReadValue("Modpack name: ", "")
			}
			if err != nil {
				return fmt.Errorf("failed to read modpack name: %w", err)
			}
		}

		author, err := cmd.Flags().GetString("author")
		if len(author) == 0 && existingPack != nil && !cmd.Flags().Changed("author") {
			author = existingPack.Author
		}
		if err != nil || len(author) == 0 {
			author, err = initReadValue("Author: ", "")
			if err != nil {
				return fmt.Errorf("failed to read author: %w", err)
			}
		}

		version, err := cmd.Flags().GetString("version")
		if len(version) == 0 && existingPack != nil && !cmd.Flags().Changed("version") {
			version = existingPack.Version
		}
		if err != nil || len(version) == 0 {
			version, err = initReadValue("Version [1.0.0]: ", "1.0.0")
			if err != nil {
				return fmt.Errorf("failed to read version: %w", err)
			}
		}

		mcVersions, err := cmdshared.GetValidMCVersions()
		if err != nil {
			return fmt.Errorf("failed to get latest minecraft versions: %w", err)
		}

		mcVersion := viper.GetString("init.mc-version")
		if len(mcVersion) == 0 && existingPack != nil && !viper.GetBool("init.latest") && !cmd.Flags().Changed("mc-version") {
			mcVersion = existingPack.Versions["minecraft"]
		}
		if len(mcVersion) == 0 {
			var latestVersion string
			if viper.GetBool("init.snapshot") {
				latestVersion = mcVersions.Latest.Snapshot
			} else {
				latestVersion = mcVersions.Latest.Release
			}
			if viper.GetBool("init.latest") {
				mcVersion = latestVersion
			} else {
				mcVersion, err = initReadValue("Minecraft version ["+latestVersion+"]: ", latestVersion)
				if err != nil {
					return fmt.Errorf("failed to read Minecraft version: %w", err)
				}
			}
		}
		if err := mcVersions.CheckValid(mcVersion); err != nil {
			return err
		}

		modLoaderName := strings.ToLower(viper.GetString("init.modloader"))
		modLoaderVersions := make(map[string]string)
		preserveExistingVersions := existingPack != nil && len(modLoaderName) == 0 && !cmd.Flags().Changed("modloader")
		if preserveExistingVersions {
			modLoaderName, modLoaderVersions = existingPackLoaderDefaults(*existingPack)
		}
		if len(modLoaderName) == 0 {
			modLoaderName, err = initReadValue("Mod loader [quilt]: ", "quilt")
			if err != nil {
				return fmt.Errorf("failed to read mod loader: %w", err)
			}
			modLoaderName = strings.ToLower(modLoaderName)
		}

		loader, ok := core.ModLoaders[modLoaderName]
		if modLoaderName != "none" {
			if ok {
				versionData, err := core.DoQuery(core.MakeQuery(loader, mcVersion))
				if err != nil {
					return fmt.Errorf("error loading %s versions: %w", loader.FriendlyName, err)
				}
				componentVersion := viper.GetString("init." + loader.Name + "-version")
				if len(componentVersion) == 0 && existingPack != nil && !cmd.Flags().Changed(loader.Name+"-version") && !viper.GetBool("init."+loader.Name+"-latest") {
					componentVersion = existingPack.Versions[loader.Name]
				}
				if len(componentVersion) == 0 {
					if viper.GetBool("init." + loader.Name + "-latest") {
						componentVersion = versionData.Latest
					} else {
						componentVersion, err = initReadValue(loader.FriendlyName+" version ["+versionData.Latest+"]: ", versionData.Latest)
						if err != nil {
							return fmt.Errorf("failed to read %s version: %w", loader.FriendlyName, err)
						}
					}
				}
				v := componentVersion
				// Forge uses a format where they prefix their version with their supported minecraft version. NeoForge
				// did this too, but only during the 1.20.1 days, they've since switched formats.
				if loader.Name == "forge" || (loader.Name == "neoforge" && mcVersion == "1.20.1") {
					v = cmdshared.GetRawForgeVersion(componentVersion)
				}
				if !slices.Contains(versionData.Versions, v) {
					return fmt.Errorf("given %s version %q cannot be found", loader.FriendlyName, v)
				}
				modLoaderVersions[loader.Name] = v
			} else {
				loader_names := slices.Collect(maps.Keys(core.ModLoaders))
				return fmt.Errorf("given mod loader %q is not supported (supported: %s; use \"none\" for no modloader)", modLoaderName, strings.Join(loader_names, ", "))
			}
		}

		indexFilePath := viper.GetString("init.index-file")
		_, err = os.Stat(indexFilePath)
		if os.IsNotExist(err) {
			// Create file
			err = os.WriteFile(indexFilePath, []byte{}, 0644)
			if err != nil {
				return fmt.Errorf("error creating index file: %w", err)
			}
			fmt.Println(indexFilePath + " created!")
		} else if err != nil {
			return fmt.Errorf("error checking index file: %w", err)
		}

		// Create the pack
		pack := core.Pack{
			Name:       name,
			Author:     author,
			Version:    version,
			PackFormat: core.CurrentPackFormat,
			Index: struct {
				File       string `toml:"file"`
				HashFormat string `toml:"hash-format"`
				Hash       string `toml:"hash,omitempty"`
			}{
				File: indexFilePath,
			},
			Versions: map[string]string{
				"minecraft": mcVersion,
			},
		}
		maps.Copy(pack.Versions, modLoaderVersions)

		// Refresh the index and pack
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
		fmt.Println(viper.GetString("pack-file") + " created!")
		return nil
	},
}

func existingPackLoaderDefaults(pack core.Pack) (string, map[string]string) {
	versions := make(map[string]string, len(pack.Versions))
	for name, version := range pack.Versions {
		if name != "minecraft" {
			versions[name] = version
		}
	}

	loaderNames := slices.Collect(maps.Keys(core.ModLoaders))
	slices.Sort(loaderNames)
	for _, name := range loaderNames {
		if _, ok := versions[name]; ok {
			return name, versions
		}
	}
	return "none", versions
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().String("name", "", "The name of the modpack (omit to define interactively)")
	initCmd.Flags().String("author", "", "The author of the modpack (omit to define interactively)")
	initCmd.Flags().String("version", "", "The version of the modpack (omit to define interactively)")
	initCmd.Flags().String("index-file", "index.toml", "The index file to use")
	_ = viper.BindPFlag("init.index-file", initCmd.Flags().Lookup("index-file"))
	initCmd.Flags().String("mc-version", "", "The Minecraft version to use (omit to define interactively)")
	_ = viper.BindPFlag("init.mc-version", initCmd.Flags().Lookup("mc-version"))
	initCmd.Flags().BoolP("latest", "l", false, "Automatically select the latest version of Minecraft")
	_ = viper.BindPFlag("init.latest", initCmd.Flags().Lookup("latest"))
	initCmd.Flags().BoolP("snapshot", "s", false, "Use the latest snapshot version with --latest")
	_ = viper.BindPFlag("init.snapshot", initCmd.Flags().Lookup("snapshot"))
	initCmd.Flags().BoolP("reinit", "r", false, "Recreate the pack file using existing values as defaults")
	_ = viper.BindPFlag("init.reinit", initCmd.Flags().Lookup("reinit"))
	initCmd.Flags().String("modloader", "", "The mod loader to use (omit to define interactively)")
	_ = viper.BindPFlag("init.modloader", initCmd.Flags().Lookup("modloader"))

	// ok this is epic
	for _, loader := range core.ModLoaders {
		initCmd.Flags().String(loader.Name+"-version", "", "The "+loader.FriendlyName+" version to use (omit to define interactively)")
		_ = viper.BindPFlag("init."+loader.Name+"-version", initCmd.Flags().Lookup(loader.Name+"-version"))
		initCmd.Flags().Bool(loader.Name+"-latest", false, "Automatically select the latest version of "+loader.FriendlyName)
		_ = viper.BindPFlag("init."+loader.Name+"-latest", initCmd.Flags().Lookup(loader.Name+"-latest"))
	}
}

func initReadValue(prompt string, def string) (string, error) {
	fmt.Print(prompt)
	if viper.GetBool("non-interactive") {
		fmt.Printf("%s\n", def)
		return def, nil
	}
	value, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("error reading input: %w", err)
	}
	// Trims both CR and LF
	value = strings.TrimSpace(strings.TrimRight(value, "\r\n"))
	if len(value) > 0 {
		return value, nil
	}
	return def, nil
}
