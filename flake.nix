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
      releaseVersion = "0.2.0";
      releaseHashes = {
        "x86_64-linux" = "sha256-p4/UNRX+rIb7VSv2JzrrxxnFd116KudCNJ/Eqtav0Z0=";
        "aarch64-linux" = "sha256-nwmYVuDemZyeE8UYGu9Ksn2PHSjlW4gIQ5ZUosMgtyw=";
        "x86_64-darwin" = "sha256-zhMoPTUlglgKdqR7BBF+AFp3+FunTmHxDjCyytP7kXY=";
        "aarch64-darwin" = "sha256-dNfZ/4yw5m8UVazVHuegvBGlNXzfPN/JKgzECsAWVwE=";
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
