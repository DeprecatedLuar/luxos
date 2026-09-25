# Settings for this physical computer. Hand-owned: luxos creates this file
# once when it is missing and never edits it again. Only what exists
# because of this box belongs here; the machine's own settings live in
# machine.nix.
{ ... }:

{
  # How long PID 1 may go without pinging the hardware watchdog before the
  # machine is reset. Without a watchdog device systemd ignores it.
  systemd.settings.Manager.RuntimeWatchdogSec = "20s";

  # How far back generations are kept, bounded by this computer's disk.
  # Collection is weekly; raise or lower the age, or set automatic = false
  # to collect only by hand.
  nix.gc = {
    automatic = true;
    dates = "weekly";
    options = "--delete-older-than 14d";
  };
}
