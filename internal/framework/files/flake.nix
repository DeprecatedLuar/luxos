# The luxos flake bootstrap, a text/template rendered per host into the staging
# tree and regenerated in place by `nix run .#write-flake` (flake-file) from the
# modules' input declarations. flake-file is pinned here; nixpkgs comes from the
# host's machine.nix.
{
  outputs = inputs: import ./framework/outputs.nix inputs;

  inputs = {
    flake-file.url = "github:denful/flake-file/eccac77d2c2567efb6f368330930f3d55af8668a";
    nixpkgs.url = "{{.Nixpkgs}}";
  };
}
