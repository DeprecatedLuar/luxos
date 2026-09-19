# The luxos flake bootstrap. Written into the staging tree and regenerated in
# place by `nix run .#write-flake` (flake-file) from the modules' input
# declarations. This is the one place flake-file's revision and the default
# nixpkgs are pinned; flake-file.nix reads them back from here.
{
  outputs = inputs: import ./framework/outputs.nix inputs;

  inputs = {
    flake-file.url = "github:denful/flake-file/eccac77d2c2567efb6f368330930f3d55af8668a";
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };
}
