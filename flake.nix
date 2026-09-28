{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # This maps to https://github.com/NixOS/nixpkgs/tree/nixos-unstable
  # The `url` option is the pattern of `github:USER_OR_ORG/REPO/BRANCH`

  outputs =
    { nixpkgs, ... }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Import nixpkgs' package set for each system.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });

      # Bump these when a tagged release is published; hashes are for the release archives.
      releaseVersion = "0.1.0";
      releaseHashes = {
        "x86_64-linux" = "sha256-7PsDFMUSjp6r5Gb0OxsV+v+4dm1wGToPFPUkNTroFgA=";
        "aarch64-linux" = "sha256-CEr8cAQqwpAw8W5LHMn2mToYKapt3gA3oPBYFBDQZfs=";
        "x86_64-darwin" = "sha256-jslOMwiVnxeHX4cN61cBkjdDPP9XUidivPxOyjfS7cg=";
        "aarch64-darwin" = "sha256-B993le/KTgMKRLLWxEM5Yf2wc28dTpu+yhLOok+2nGo=";
      };
    in
    {
      packages = forAllSystems (
        system:
        let pkgs = nixpkgsFor.${system};
        in
        rec {
          packwiz = pkgs.callPackage ./nix {
            version = releaseVersion;
            hashes = releaseHashes;
          };
          default = packwiz;
        }
      );

      formatter = forAllSystems (system: nixpkgsFor.${system}.nixfmt);
    };
}
