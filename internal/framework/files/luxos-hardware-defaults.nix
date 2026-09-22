{ lib, config, ... }:

let
  # nixpkgs' own nvidia.nix sets hardware.nvidia.prime.offload.enable via
  # mkDefault (reverseSyncCfg.enable), so a plain mkDefault true here would
  # conflict at equal priority. This priority must stay below mkDefault's
  # 1000 (so it wins over nixpkgs' default) and above a plain definition's
  # 100 (so a host module setting the option directly still wins).
  offloadPriority = 900;

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
  ];
}
