# LUXOS property - keep walking buddy
# Vendored into every user unit; luxos restores this file. Put your own values in this unit's modules/ folder.
{ config, lib, ... }:

let
  name = baseNameOf ./.;
in
{
  users.users.${name} = {
    isNormalUser = true;
    extraGroups = config.${name}.groups;
    openssh.authorizedKeys.keyFiles = lib.optional (builtins.pathExists ./authorized_keys) ./authorized_keys;
  };

  luxos.users.${name}.password = ./.password.age;
}
