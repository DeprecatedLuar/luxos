# This computer's selection: modules every host built on it imports.
# Its own modules as ./name.nix, shared ones as ../category/name.nix.
{ ... }:
{
  imports = [
    ./boot.nix
  ];
}
