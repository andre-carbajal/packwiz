{
  lib,
  stdenv,
  fetchurl,
  installShellFiles,
  version,
  hashes,
}:
let
  assetSuffixes = {
    "x86_64-linux" = "linux_amd64";
    "aarch64-linux" = "linux_arm64";
    "x86_64-darwin" = "darwin_amd64";
    "aarch64-darwin" = "darwin_arm64";
  };
  system = stdenv.hostPlatform.system;
  suffix = assetSuffixes.${system} or (throw "packwiz has no release binary for ${system}");
  hash = hashes.${system} or (throw "packwiz has no release hash for ${system}");
  archive = "packwiz_${version}_${suffix}.tar.gz";
in
stdenv.mkDerivation {
  pname = "packwiz";
  inherit version;

  src = fetchurl {
    url = "https://github.com/andre-carbajal/packwiz/releases/download/v${version}/${archive}";
    inherit hash;
  };
  sourceRoot = ".";
  dontConfigure = true;
  dontBuild = true;

  nativeBuildInputs = [
    installShellFiles
  ];

  installPhase = ''
    runHook preInstall
    mkdir -p $out/bin
    install -m 755 packwiz $out/bin/packwiz
    installShellCompletion --cmd packwiz \
      --bash <($out/bin/packwiz completion bash) \
      --fish <($out/bin/packwiz completion fish) \
      --zsh <($out/bin/packwiz completion zsh)
    runHook postInstall
  '';

  meta = with lib; {
    description = "A command line tool for editing and distributing Minecraft modpacks, using a git-friendly TOML format";
    homepage = "https://packwiz.infra.link/";
    license = licenses.mit;
    mainProgram = "packwiz";
    platforms = attrNames hashes;
  };
}
