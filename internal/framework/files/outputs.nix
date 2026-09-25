# The flake's outputs function: evaluates flake-file.nix (and, through it,
# the host's selected modules) as flake-file modules. Shared by the static
# flake.nix and by the flake.nix write-flake regenerates (flake-file.outputs),
# so the two can never drift. luxos.modules must be a specialArg because
# modules use it in `imports`, which cannot depend on _module.args.
inputs:
let
  inherit (inputs.nixpkgs) lib;
in
(lib.evalModules {
  specialArgs = {
    inherit inputs;
    inherit (inputs) self;
    luxos.modules = import ./units.nix {
      inherit lib;
      root = ../config/modules;
    };
  };
  modules = [
    inputs.flake-file.flakeModules.flake
    ./flake-file.nix
  ];
}).config.outputs
  inputs
