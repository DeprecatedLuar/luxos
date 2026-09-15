{ pkgs, luxos, ... }:

{
  imports = luxos.modules [ "desktop" ];

  environment.systemPackages = with pkgs; [
    wl-clipboard
    swaybg
    wtype
  ];
}
