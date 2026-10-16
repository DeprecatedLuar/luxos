# LUXOS property - keep walking buddy
# Vendored into every user unit; luxos restores this file. Set the groups in this host's settings file.
{ lib, ... }:

let
  name = baseNameOf ./.;
in
{
  options.${name}.groups = lib.mkOption {
    type = lib.types.listOf lib.types.str;
    default = [ ];
    description = "Supplementary groups of this user on this host";
  };
}
