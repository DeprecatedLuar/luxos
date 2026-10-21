{ ... }:

# Reboot rather than limp on: for unattended machines or chasing freezes.
# Select it per computer with `luxos module enable auto-reboot --machine`.
{
  # A frozen kernel leaves no logs; panicking is what gets a crash into pstore.
  boot.kernel.sysctl = {
    "kernel.panic_on_oops" = 1;
    "kernel.hardlockup_panic" = 1;
  };

  # Resets a hung machine; systemd ignores it without a watchdog device.
  systemd.settings.Manager.RuntimeWatchdogSec = "20s";
}
