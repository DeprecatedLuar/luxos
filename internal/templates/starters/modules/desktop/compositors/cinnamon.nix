{ lib, luxos, ... }:
{
  imports = luxos.modules [ "x11" ];

  services.xserver.desktopManager.cinnamon.enable = true;
}
