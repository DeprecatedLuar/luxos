# The luxos flake bootstrap, a text/template rendered per host into the staging
# tree and regenerated in place by `nix run .#write-flake` (flake-file) from the
# modules' input declarations. flake-file is pinned here; every other input,
# nixpkgs included, is derived from the selected modules' own
# flake-file.inputs declarations (nixsrc.RenderInputs), so a module needs
# nothing beyond its own file to add one, even on the input's first build.
{
  outputs = inputs: import ./framework/outputs.nix inputs;

  inputs = {
    flake-file.url = "github:denful/flake-file/eccac77d2c2567efb6f368330930f3d55af8668a";
{{.Inputs}}  };
}
