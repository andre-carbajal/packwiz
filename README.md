# packwiz

**This repository is a fork of the original [`packwiz/packwiz`](https://github.com/packwiz/packwiz) project.**

**packwiz is a command-line tool for creating and maintaining Minecraft modpacks.** It stores pack information as TOML
metadata, so you can track changes with Git and share a reproducible pack without committing every downloaded mod file.

Use packwiz to build a modpack, keep its mods up to date, and export it for CurseForge or Modrinth. It is not a game
launcher; players can install and update a pack
with [packwiz-installer](https://packwiz.infra.link/tutorials/installing/packwiz-installer/).

## Install

Download a prebuilt binary from [GitHub Releases](https://github.com/andre-carbajal/packwiz/releases/latest), or use a
package manager:

**macOS or Linux (Homebrew):**

```sh
brew install --cask andre-carbajal/tap/packwiz
```

**Windows (Scoop):**

```powershell
scoop bucket add andre-carbajal https://github.com/andre-carbajal/scoop-bucket
scoop install packwiz
```

**Linux or macOS (Nix flakes):**

```sh
nix profile install github:andre-carbajal/packwiz#packwiz
```

## Get started

Create a folder for your pack, initialize it, and add a mod:

```sh
mkdir my-modpack
cd my-modpack
packwiz init
packwiz modrinth add sodium
```

Then review the pack, update its mods, or export it:

```sh
packwiz list
packwiz update --all
packwiz modrinth export
```

Run packwiz commands from the folder containing `pack.toml`.

## What packwiz manages

- `pack.toml` stores the pack name, version, Minecraft version, and mod loader.
- `index.toml` tracks the pack's files.
- `*.pw.toml` files store metadata for externally managed files, including their source and update settings.
- Local files such as configuration and resource files can also be included in the pack.

These small metadata files are what you normally commit to Git; downloaded CurseForge or Modrinth JARs do not need to be
committed.

## Add and link mods

Add mods from Modrinth or CurseForge using a project URL, slug, ID, or search:

```sh
packwiz modrinth add sodium
packwiz curseforge add sodium
```

When the same file is available on both platforms, `packwiz link` can record both references. A link is accepted only
when the file contents are identical; matching names or version numbers are not enough.

```sh
packwiz link sodium
packwiz link --all
```

## Documentation

The [repository Wiki](https://github.com/andre-carbajal/packwiz/wiki) has user guides
for [getting started](https://github.com/andre-carbajal/packwiz/wiki/Getting-Started), [importing and exporting packs](https://github.com/andre-carbajal/packwiz/wiki/Importing-and-Exporting),
and the complete [command reference](https://github.com/andre-carbajal/packwiz/wiki/Commands). For options available in
your installed version, run `packwiz --help` or `packwiz <command> --help`.

For player installation and automatic updates, see the
official [packwiz-installer guide](https://packwiz.infra.link/tutorials/installing/packwiz-installer/).

## Building from source

Building packwiz requires Go 1.26.8 or newer. Local builds need `CURSEFORGE_API_KEY` for CurseForge API requests,
including some cross-platform link lookups. Official release and snapshot binaries embed a key configured at build time;
a key in a distributed binary can be extracted and must be treated as public, not as a secret.
