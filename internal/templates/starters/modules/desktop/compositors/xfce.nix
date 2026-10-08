{ lib, luxos, ... }:
{
  imports = luxos.modules [ "x11" ];

  services.xserver.desktopManager.xfce.enable = true;

  # Block these autostart-generated systemd units so XFCE's own daemons
  # don't leak into other sessions. xfce4-session runs its own autostart
  # scan independently, so this doesn't affect the XFCE session itself.
  systemd.user.services = lib.genAttrs [
    "app-xfsettingsd@autostart"
    "app-xfce4-power-manager@autostart"
    "app-xfce4-screensaver@autostart"
    "app-xfce4-notifyd@autostart"
  ] (_: { enable = false; });
}
