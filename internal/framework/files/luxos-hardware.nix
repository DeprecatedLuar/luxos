{ lib, ... }:

{
  options.luxos.hardware.gpus = lib.mkOption {
    type = lib.types.listOf (lib.types.submodule {
      options = {
        busId = lib.mkOption { type = lib.types.str; };
        vendor = lib.mkOption { type = lib.types.str; };
        vendorId = lib.mkOption { type = lib.types.str; };
        class = lib.mkOption { type = lib.types.enum [ "vga" "3d" ]; };
        bootVga = lib.mkOption { type = lib.types.bool; };
      };
    });
    default = [ ];
    description = "PCI display-class devices detected by luxos; written to framework/gpu.nix on every rebuild. Read-only facts - do not set.";
  };
}
