{ lib, ... }:

{
  options.luxos.hardware.gpus = lib.mkOption {
    type = lib.types.listOf (lib.types.submodule {
      options = {
        busId = lib.mkOption { type = lib.types.str; };
        vendor = lib.mkOption { type = lib.types.str; };
        vendorId = lib.mkOption { type = lib.types.str; };
        deviceId = lib.mkOption { type = lib.types.str; };
        class = lib.mkOption { type = lib.types.enum [ "vga" "3d" ]; };
        bootVga = lib.mkOption { type = lib.types.bool; };
      };
    });
    default = [ ];
    description = "PCI display-class devices detected by luxos; written to framework/hardware-facts.nix on every rebuild. Read-only facts - do not set.";
  };

  options.luxos.hardware.vendor = lib.mkOption {
    type = lib.types.str;
    description = "System vendor as the firmware reports it (DMI sys_vendor); written to framework/hardware-facts.nix on every rebuild. Read-only fact - do not set.";
  };
  options.luxos.hardware.product = lib.mkOption {
    type = lib.types.str;
    description = "Product name as the firmware reports it (DMI product_name); written to framework/hardware-facts.nix on every rebuild. Read-only fact - do not set.";
  };
  options.luxos.hardware.chassisType = lib.mkOption {
    type = lib.types.int;
    description = "SMBIOS chassis type (DMI chassis_type): 3 desktop; 8, 9, 10, 14 portable/laptop/notebook; 31 convertible. Written to framework/hardware-facts.nix on every rebuild. Read-only fact - do not set.";
  };
  options.luxos.hardware.platformProfiles = lib.mkOption {
    type = lib.types.listOf lib.types.str;
    default = [ ];
    description = "Platform profiles the firmware accepts (platform_profile_choices), empty when the computer has no platform-profile driver; written to framework/hardware-facts.nix on every rebuild. Read-only fact - do not set.";
  };
}
