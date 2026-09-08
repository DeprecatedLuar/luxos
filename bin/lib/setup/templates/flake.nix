{
  description = "luxos machine configurations";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  outputs = { self, nixpkgs, ... }@inputs:
  let
    mkHost = hostName: nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      # frameworkModules lets a machine module reach a framework module by
      # name (frameworkModules + "/wayland.nix") regardless of how deep the
      # machine module is nested — the two repos aren't nested relative to
      # each other, so no relative path between them can exist.
      specialArgs = { inherit inputs hostName; frameworkModules = ./framework/modules; };
      modules = [ ./configuration.nix ];
    };
  in {
    # One entry per machine directory under .system/machines/ — add a line
    # here when adding a machine.
    nixosConfigurations = {
      paraloid = mkHost "paraloid";
      nuremberg = mkHost "nuremberg";
      ae = mkHost "ae";
    };
  };
}
