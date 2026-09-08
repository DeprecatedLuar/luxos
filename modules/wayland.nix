{ pkgs, ... }:

{
  imports = [ ./desktop.nix ];

  environment.systemPackages = with pkgs; [
    wl-clipboard
    swaybg
    wtype
  ];
}
