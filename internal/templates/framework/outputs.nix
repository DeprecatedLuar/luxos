# The flake's outputs function: the one host this stage holds. luxos.modules
# and modulesPath-using imports resolve as specialArgs, since modules use
# luxos.modules in `imports`, which cannot depend on _module.args.
inputs: hostName:
let
  inherit (inputs.nixpkgs) lib;
  system = "x86_64-linux";
  luxos.modules = import ./units.nix {
    inherit lib;
    root = ../config/modules;
  };
  channelOverlay = import ./overlay.nix { inherit lib inputs system; };
in
{
  nixosConfigurations.${hostName} = lib.nixosSystem {
    specialArgs = { inherit inputs luxos hostName; };
    modules = [
      ./configuration.nix
      { nixpkgs.hostPlatform = system; nixpkgs.overlays = [ channelOverlay ]; }
    ];
  };
}
