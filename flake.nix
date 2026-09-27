{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # This maps to https://github.com/NixOS/nixpkgs/tree/nixos-unstable
  # The `url` option is the pattern of `github:USER_OR_ORG/REPO/BRANCH`

  outputs =
    {
      self,
      nixpkgs,
    }:
    with nixpkgs.lib;
    let
      # List of explicitly unsupported systems
      explicitlyUnsupportedSystems = [ ];

      # Packwiz should support all 64-bit systems supported by go, but nix only
      # support strictly less, so all nix-supported systems are included
      # (except ones in explicitlyUnsupportedSystems).
      supportedSystems =
        filter
          # Filter out systems that are explicitly supported
          (s: !elem s explicitlyUnsupportedSystems)
          # This lists all systems reasonably well-supported by nix
          (import "${nixpkgs}/lib/systems/flake-systems.nix" { });

      # Helper generating outputs for each supported system
      forAllSystems = genAttrs supportedSystems;

      # Import nixpkgs' package set for each system.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      # Packwiz package
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
          go_1_26_8 = pkgs.go_1_26.overrideAttrs (_: {
            version = "1.26.8";
            src = pkgs.fetchurl {
              url = "https://go.dev/dl/go1.26.8.src.tar.gz";
              hash = "sha256-Tjm5jkL5RvoFrIvFtxh335fb23y7Gnd7VBZnrXEX/S4=";
            };
          });
          buildGoModule = pkgs.buildGoModule.override { go = go_1_26_8; };
        in
        rec {
          packwiz = pkgs.callPackage ./nix {
            version = substring 0 8 self.rev or "dirty";
            inherit buildGoModule;
          };
          # Build packwiz by default when no package name is specified
          default = packwiz;
        }
      );

      # This flake's nix code formatter
      formatter = forAllSystems (system: nixpkgsFor.${system}.nixfmt);
    };
}
