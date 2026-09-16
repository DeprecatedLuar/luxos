{ pkgs, luxos, ... }:

{
  imports = luxos.modules [ "desktop" ];

  #──[Desktop Environment]───────────────────────────────────────────────────

  services.xserver = {
    enable = true;
    displayManager.startx.enable = true;  # Proper startx support with module paths
  };

  services.libinput.enable = true;  # X11 input driver (keyboard/mouse)

  environment.systemPackages = with pkgs; [
    xorg.xinit
    xdotool
    feh
  ];
}
