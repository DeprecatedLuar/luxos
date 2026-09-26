# Per-machine settings for this host. Hand-owned: luxos creates this file
# once when it is missing and never edits it again. Machine-specific values
# only - no imports and no paths, those belong in a module.
{ ... }:

{
  # Base channel (pkgs.nixpkgs), required.
  flake-file.inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";

  time.timeZone = "UTC";
  i18n.defaultLocale = "en_US.UTF-8";
}
