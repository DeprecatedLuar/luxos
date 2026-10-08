{ pkgs, luxos, ... }:
let
  sessionTarget = "hyprland-session.target";
  sessionEnv = "WAYLAND_DISPLAY XDG_CURRENT_DESKTOP HYPRLAND_INSTANCE_SIGNATURE";

  # called from Hyprland autostart; activates graphical-session.target without UWSM
  sessionStart = pkgs.writeShellScriptBin "hyprland-session-start" ''
    set -e
    ${pkgs.dbus}/bin/dbus-update-activation-environment --systemd ${sessionEnv}
    # stop first: a target left active by a previous login makes start a no-op
    ${pkgs.systemd}/bin/systemctl --user stop ${sessionTarget}
    ${pkgs.systemd}/bin/systemctl --user start ${sessionTarget}
  '';
in
{
  imports = luxos.modules [ "wayland" ];

  programs.hyprland.enable = true;
  programs.hyprland.package = pkgs.unstable.hyprland;
  programs.hyprland.withUWSM = true;

  systemd.user.targets.hyprland-session = {
    description = "Hyprland session";
    bindsTo = [ "graphical-session.target" ];
    wants = [ "graphical-session-pre.target" ];
    after = [ "graphical-session-pre.target" ];
  };

  environment.systemPackages = [ sessionStart ] ++ (with pkgs.unstable; [
    hyprsunset
    grimblast
    hypridle
    hyprpicker
    swayimg
    hyprpolkitagent
  ]);
}
