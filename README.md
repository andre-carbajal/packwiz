# packwiz
packwiz is a command line tool for creating Minecraft modpacks. Instead of managing JAR files directly, packwiz creates TOML metadata files which can be easily version-controlled and shared with git (see an example pack [here](https://github.com/packwiz/packwiz-example-pack)). You can then [export it to a CurseForge or Modrinth modpack](https://packwiz.infra.link/tutorials/hosting/curseforge/), or [use packwiz-installer](https://packwiz.infra.link/tutorials/installing/packwiz-installer/) for an auto-updating MultiMC instance.

packwiz is great for...

- Distributing private modpacks for servers
- Creating modpacks for CurseForge and Modrinth

packwiz is not so great for...

- Managing downloaded mod files (use [Curse/GDLauncher or another CLI](https://gist.github.com/comp500/13ae6f058221196077fb19953ac608c7))
- People who want a GUI (though there are some [third-party efforts](https://github.com/ExoPlant/packwiz-gui))

Join my Discord server if you need help [here](https://discord.gg/Csh8zbbhCt)!

## Features
- Git-friendly TOML-based metadata format
- MultiMC pack installer/updater, with support for optional mods and fast automatic updates - perfect for servers!
- Pack distribution with HTTP servers, with a built in local server for testing
- Easy installation and updating of multiple mods at once from CurseForge and Modrinth
- Exporting to CurseForge and Modrinth packs
- Importing from CurseForge and Modrinth packs
- Server-only and Client-only mod handling
- Creation of remote file metadata from JAR files for CurseForge mods

## Linking CurseForge and Modrinth

Adding a mod (including dependencies) automatically looks for **identical file
contents** on the other platform. One `.pw.toml` stores both references; names
and version numbers are never used to infer a match. Failed or ambiguous
secondary lookups warn without cancelling the primary installation.

Complete existing packs or select a specific project/file manually:

```sh
packwiz link --all
packwiz link sodium
packwiz link sodium --curseforge https://www.curseforge.com/minecraft/mc-mods/sodium/files/FILE_ID
packwiz link sodium --modrinth https://modrinth.com/mod/sodium/version/VERSION_ID
```

Manual links also require identical contents and preserve the metadata path,
download configuration, optional settings, environments and pin. `link --all`
keeps successful links and reports failures with a nonzero exit status. Linking
may download files into the existing packwiz cache; fingerprint-only files must
be downloaded from their origin to prove byte identity.

The root `update-source` field selects one primary updater (`curseforge` or
`modrinth`). Legacy metadata infers it from its sole updater or its download
mode/Modrinth CDN URL; ambiguous metadata needs an explicit value. Updating
invalidates old secondary references and searches again for the updated file.

CurseForge exports use native manifest references; Modrinth exports use native
Modrinth URLs for linked files without changing the installed download source.
Both exports fail by default if a managed file has no destination reference:

```sh
packwiz curseforge export
packwiz modrinth export
# Explicitly permit the previous external-file behavior:
packwiz curseforge export --allow-overrides
packwiz modrinth export --allow-overrides
```

Local configuration/resource overrides are unaffected. `--allow-overrides`
does not guarantee platform approval. Failed exports preserve existing output
files. Local builds require `CURSEFORGE_API_KEY` for CurseForge lookups, including
automatic links from Modrinth (see below).

## Installation
Stable prebuilt binaries are available from [GitHub Releases](https://github.com/andre-carbajal/packwiz/releases/latest).

On macOS or Linux, install with Homebrew:

```sh
brew install --cask andre-carbajal/tap/packwiz
```

On Windows, install with Scoop:

```powershell
scoop bucket add andre-carbajal https://github.com/andre-carbajal/scoop-bucket
scoop install packwiz
```

On Linux or macOS with Nix flakes enabled:

```sh
nix profile install github:andre-carbajal/packwiz#packwiz
```

Development snapshots are available from [nightly.link](https://nightly.link/andre-carbajal/packwiz/workflows/go/main).

To run the executable, first extract it, then add the folder where you extracted it to your PATH environment variable ([see tutorial for Windows here](https://www.howtogeek.com/118594/how-to-edit-your-system-path-for-easy-command-line-access/)) or move it to where you want to use it.

Building for development requires Go 1.26.8 or newer. CurseForge API access in a local build requires setting `CURSEFORGE_API_KEY`; official release and snapshot binaries embed the configured key at build time. Anyone can extract it from a distributed binary, so it must be treated as public rather than confidential.

## Documentation
See https://packwiz.infra.link/ for the full packwiz documentation!
