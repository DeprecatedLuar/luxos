# LUXOS property - keep walking buddy
# Vendored into every user unit; luxos restores this file. Put your own values in this unit's modules/ folder.
{ config, ... }:

let
  name = baseNameOf ./.;
in
{
  users.users.${name} = {
    isNormalUser = true;
    extraGroups = config.${name}.groups;
  };

  luxos.users.${name}.password = ./.password.age;
}
