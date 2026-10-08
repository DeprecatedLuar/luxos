{ config, lib, pkgs, ... }:

let
  cfg = config.laptop;
  tlpProfile = {
    performance = "PRF";
    balanced = "BAL";
    power-saver = "SAV";
  };

  # TLP's own power-saver name is rejected by firmware that lacks it, and the
  # failed write is only logged at debug level.
  offered = config.luxos.hardware.platformProfiles;
  powerSaverPlatformProfile = lib.findFirst (p: lib.elem p offered) null [ "low-power" "quiet" ];
in
{
  #──[Power Management]──────────────────────────────────────────────────────

  powerManagement.enable = true;
  services.power-profiles-daemon.enable = false;

  # Profile settings need TLP 1.10+, which only unstable ships.
  services.tlp = {
    enable = true;
    package = pkgs.unstable.tlp;
    settings = {
      TLP_AUTO_SWITCH = if cfg.autoSwitch then 1 else 0;
      TLP_PROFILE_AC = tlpProfile.${cfg.pluggedIn};
      TLP_PROFILE_BAT = tlpProfile.${cfg.onBattery};

      # All three, or turbo stays off after the first power-saver switch.
      CPU_BOOST_ON_AC = 1;
      CPU_BOOST_ON_BAT = 1;
      CPU_BOOST_ON_SAV = 0;

      CPU_ENERGY_PERF_POLICY_ON_AC = "performance";
      CPU_ENERGY_PERF_POLICY_ON_BAT = "balance_power";
      CPU_ENERGY_PERF_POLICY_ON_SAV = "power";
    } // lib.optionalAttrs (powerSaverPlatformProfile != null) {
      PLATFORM_PROFILE_ON_SAV = powerSaverPlatformProfile;
    };
  };

  environment.systemPackages = [ pkgs.unstable.tlp-pd ];
  systemd.packages = [ pkgs.unstable.tlp-pd ];
  systemd.services.tlp-pd.wantedBy = [ "graphical.target" ];

  #──[Lid Switch]────────────────────────────────────────────────────────────

  services.logind.settings.Login = {
    HandleLidSwitch = cfg.lidSwitch;
    HandleLidSwitchExternalPower = cfg.lidSwitch;
    HandleLidSwitchDocked = "ignore";
  };
}
