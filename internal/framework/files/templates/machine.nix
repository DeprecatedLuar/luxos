# Per-machine settings for this host. Hand-owned: luxos creates this file
# once when it is missing and never edits it again. Machine-specific values
# only - no imports and no paths, those belong in a module.
{ ... }:

{
  time.timeZone = "UTC";
  i18n.defaultLocale = "en_US.UTF-8";

  # How far back generations are kept. Collection is weekly; raise or lower
  # the age, or set automatic = false to collect only by hand.
  nix.gc = {
    automatic = true;
    dates = "weekly";
    options = "--delete-older-than 14d";
  };
}
