{ pkgs, ... }:

{
  environment.systemPackages = with pkgs; [
    kitty
    firefox
    libnotify
    playerctl
    celluloid
    mpv
    xfce.tumbler
    ffmpegthumbnailer
    adwaita-icon-theme
  ];
}
