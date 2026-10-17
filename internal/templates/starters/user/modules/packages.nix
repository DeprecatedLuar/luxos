{ pkgs, ... }:

let
  name = baseNameOf ../.;
in
{
  users.users.${name}.packages = with pkgs; [
    # cowsay
  ];
}
