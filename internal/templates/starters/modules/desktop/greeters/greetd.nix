{ config, pkgs, ... }:

let
  sessions = config.services.displayManager.sessionData.desktops;
in
{
  services.greetd = {
    enable = true;
    settings.default_session = {
      command = "${pkgs.tuigreet}/bin/tuigreet --greeting '★·.·´¯`·.·★·.·´¯`·.·★·.·´¯`·.·★·.·´¯`·.·★' --asterisks --remember --remember-session --time --sessions ${sessions}/share/wayland-sessions --xsessions ${sessions}/share/xsessions";
      user = "greeter";
    };
  };
}
