{ luxos, ... }:
{
  imports = luxos.modules [ "x11" ];

  services.xserver.displayManager.lightdm.enable = true;
}
