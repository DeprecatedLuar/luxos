{ lib, pkgs, luxos, ... }:

{
  imports = luxos.modules [ "desktop" ];

  environment.sessionVariables.NIXOS_OZONE_WL = lib.mkDefault "1";

  environment.systemPackages = with pkgs; [
    wl-clipboard
    wlopm
    wtype
    ydotool
  ];
}
