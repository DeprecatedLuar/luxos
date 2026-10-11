{ lib, ... }:

let
  profile = lib.types.enum [ "performance" "balanced" "power-saver" ];
  turboOption = default: lib.mkOption {
    type = lib.types.bool;
    inherit default;
    description = "CPU turbo boost while this profile is active";
  };
in
{
  options.laptop.pluggedIn = lib.mkOption {
    type = profile;
    default = "balanced";
    description = "performance | balanced | power-saver";
  };

  options.laptop.onBattery = lib.mkOption {
    type = profile;
    default = "power-saver";
    description = "performance | balanced | power-saver";
  };

  options.laptop.autoSwitch = lib.mkOption {
    type = lib.types.bool;
    default = true;
    description = "Switches power mode if plugged in vs on battery";
  };

  # `turbo on|off` overrides these until the next profile switch.
  options.laptop.turboPerformance = turboOption true;
  options.laptop.turboBalanced = turboOption true;
  options.laptop.turboPowerSaver = turboOption false;

  options.laptop.lidSwitch = lib.mkOption {
    type = lib.types.str;
    default = "suspend";
    description = "systemd-logind action when the lid closes.";
  };
}
