{ config, pkgs, ... }:

{
  #──[Gaming Kernel Optimizations]────────────────────────────────────

  boot.kernel.sysctl = {
    "vm.swappiness" = 10;
    "vm.compaction_proactiveness" = 0;
    "vm.watermark_boost_factor" = 1;
    "vm.min_free_kbytes" = 1048576;
    "vm.watermark_scale_factor" = 500;
    "vm.zone_reclaim_mode" = 0;
    "kernel.sched_autogroup_enabled" = 1;
    "kernel.sched_cfs_bandwidth_slice_us" = 3000;
  };

  #──[Services]───────────────────────────────────────────────────────

  programs.gamemode = {
    enable = true;
    settings = {
      general = {
        renice = 10;
        inhibit_screensaver = 1;
      };
    };
  };

  programs.steam = {
    enable = true;
    remotePlay.openFirewall = true;
    dedicatedServer.openFirewall = true;
    extraPackages = with pkgs; [ icu ];  # Required for .NET games (tModLoader, etc.)
    extraCompatPackages = [ pkgs.proton-ge-bin ];
  };

  #──[Graphics]──────────────────────────────────────────────────────────────

  hardware.graphics = {
    enable = true;
    enable32Bit = true;  # Required for 32-bit games and Windows games via Proton
  };

  #──[Gaming Packages]───────────────────────────────────────────────────────

  environment.systemPackages = with pkgs; [
    #lutris
    #heroic

    mangohud
    gamemode
    protonup-qt
    gamescope
    umu-launcher

  ];
}
