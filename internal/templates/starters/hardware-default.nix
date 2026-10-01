# This computer's hardware module. Hand-owned: luxos creates this file once
# when it is missing and never edits it again. Drop a line to stop importing
# that file; add your own files to this folder and import them here.
{
  imports = [
    ./hardware-configuration.nix
    ./boot.nix
    ./hardware.nix
  ];
}
