{ lib, config, pkgs, ... }:

let
  # nixpkgs' own nvidia.nix sets hardware.nvidia.prime.offload.enable via
  # mkDefault (reverseSyncCfg.enable), so a plain mkDefault true here would
  # conflict at equal priority. This priority must stay below mkDefault's
  # 1000 (so it wins over nixpkgs' default) and above a plain definition's
  # 100 (so a host module setting the option directly still wins).
  offloadPriority = 900;

  generations = import ./nvidia-generations.nix;

  # Packages the pinned nixpkgs offers; tested by attribute, never assumed.
  nvidiaPackages = config.boot.kernelPackages.nvidiaPackages;
  legacyDriverPackage = "legacy_580";

  gpus = config.luxos.hardware.gpus;

  nvidiaGpus = builtins.filter (g: g.vendor == "nvidia") gpus;
  otherGpus = builtins.filter (g: g.vendor != "nvidia") gpus;
  bootVgaOthers = builtins.filter (g: g.bootVga) otherGpus;

  nvidia =
    if builtins.length nvidiaGpus == 1 then builtins.elemAt nvidiaGpus 0
    else null;

  # The integrated-GPU candidate: the sole non-NVIDIA GPU if there is
  # exactly one, else the sole one with bootVga = true, else undetectable.
  integrated =
    if builtins.length otherGpus == 1 then builtins.elemAt otherGpus 0
    else if builtins.length bootVgaOthers == 1 then builtins.elemAt bootVgaOthers 0
    else null;

  havePair = nvidia != null && integrated != null;

  legacyIds = lib.concatLists (lib.attrValues generations.legacy);
  isLegacy = g: lib.elem g.deviceId legacyIds;
  isMaxwellToVolta = g: lib.elem g.deviceId generations.maxwellToVolta;
  # NVIDIA's installer rule: any pre-Turing GPU in the machine means the
  # proprietary kernel module, legacy cards included.
  isPreTuring = g: isLegacy g || isMaxwellToVolta g;
  legacyGpus = builtins.filter isLegacy nvidiaGpus;
  supportedNvidia = builtins.filter (g: !isLegacy g) nvidiaGpus;

  legacyBranch = g:
    lib.findFirst (b: lib.elem g.deviceId generations.legacy.${b}) null (lib.attrNames generations.legacy);

  legacyWarning = g:
    let
      branch = legacyBranch g;
      attr = "legacy_${branch}";
      what = "NVIDIA GPU ${g.busId} (${g.deviceId}) is only supported by the legacy ${branch}.xx driver, which luxos does not enable automatically.";
    in
    if nvidiaPackages ? ${attr}
    then "${what} To use it, add to this computer's hardware.nix: hardware.nvidia.package = config.boot.kernelPackages.nvidiaPackages.${attr}; services.xserver.videoDrivers = [ \"nvidia\" ];"
    else "${what} nixpkgs packages no driver for it.";
in
{
  # Each branch is its own lib.mkIf with a statically-named option path.
  # A single mkIf whose content used a dynamic attribute name (keyed on
  # integrated.vendor) would force that name while the module system
  # merges definitions structurally, even when the branch's condition is
  # false - forcing config.luxos.hardware.gpus (this module's own `config`
  # argument) during that structural pass is an infinite recursion. `&&`
  # short-circuits, so integrated.vendor is only forced once havePair is
  # already known true.
  config = lib.mkMerge [
    (lib.mkIf havePair {
      hardware.nvidia.prime = {
        nvidiaBusId = lib.mkDefault nvidia.busId;
        offload.enable = lib.mkOverride offloadPriority true;
        offload.enableOffloadCmd = lib.mkOverride offloadPriority true;
      };
    })
    (lib.mkIf (havePair && integrated.vendor == "intel") {
      hardware.nvidia.prime.intelBusId = lib.mkDefault integrated.busId;
    })
    (lib.mkIf (havePair && integrated.vendor != "intel") {
      hardware.nvidia.prime.amdgpuBusId = lib.mkDefault integrated.busId;
    })
    (lib.mkIf (supportedNvidia != [ ]) {
      services.xserver.videoDrivers = lib.mkDefault [ "nvidia" ];
      hardware.nvidia.modesetting.enable = lib.mkDefault true;
      hardware.nvidia.open = lib.mkDefault (!(lib.any isPreTuring nvidiaGpus));
    })
    (lib.mkIf (lib.any isMaxwellToVolta supportedNvidia && nvidiaPackages ? ${legacyDriverPackage}) {
      hardware.nvidia.package = lib.mkDefault nvidiaPackages.${legacyDriverPackage};
    })
    (lib.mkIf (legacyGpus != [ ]) {
      warnings = map legacyWarning legacyGpus;
    })
    (lib.mkIf (builtins.any (g: g.vendor == "intel") gpus) {
      hardware.graphics.extraPackages = [ pkgs.intel-media-driver pkgs.intel-vaapi-driver ];
      hardware.graphics.extraPackages32 = [ pkgs.pkgsi686Linux.intel-media-driver pkgs.pkgsi686Linux.intel-vaapi-driver ];
    })
  ];
}
