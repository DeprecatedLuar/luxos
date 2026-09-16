{ pkgs, ... }:

{
  #──[Packages]───────────────────────────────────────────────────────────────
  environment.systemPackages = with pkgs; [
    # Machine-specific packages
  ];

  #──[Keyboard / Input]───────────────────────────────────────────────────────
  services.xserver.xkb = {
    layout = "us";
    variant = "";
  };

  #──[Network]────────────────────────────────────────────────────────────────
  networking.networkmanager.enable = true;
}
