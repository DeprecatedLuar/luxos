{ pkgs, luxos, ... }:

{
  imports = luxos.modules [ "desktop" ];

  environment.systemPackages = with pkgs; [
    wl-clipboard
    wlopm
    wtype
    qt6Packages.qt6ct
    ydotool
  ];
}
