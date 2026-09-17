{ pkgs, luxos, ... }:

{
  imports = luxos.modules [ "desktop" ];

  environment.systemPackages = with pkgs; [
    wl-clipboard
    wtype
    qt6Packages.qt6ct
    ydotool
  ];
}
