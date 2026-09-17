{ pkgs, lib, inputs, config, ... }:

{
  imports = [
    ../hardware-configuration.nix  # Auto-generated filesystems
  ];

  #──[Packages]──────────────────────────────────────────────────────────────

  nixpkgs.config.allowUnfree = true;

  environment.systemPackages = with pkgs; [
    ncdu
    fastfetch

    ripgrep
    git
    wget
    lsof
    ffmpeg
    fd
    jq
    tmux
    at
    lm_sensors
    pciutils
    zip
    unzip
    dnsutils
    file
    nmap
    socat
    tcpdump
    entr
    tree
    squashfsTools

    kitty.terminfo
  ];

  programs.nix-ld.enable = true;

  # Truth source for `lux module`/`lux user`'s state markers
  # (implementation-plan.md #23, Phase 5): the running generation's copy of
  # the staged entrypoint, read at /run/current-system/etc/luxos/modules.nix.
  # $STAGING_DIR is rewritten by dry-build and by failed builds, so it can't
  # say what's actually running — this file only updates on a real switch.
  environment.etc."luxos/modules.nix".source = ../config/modules/default.nix;

  #──[Users]─────────────────────────────────────────────────────────────────

  users.users.root.hashedPassword = "!"; # No password login for root; use sudo or SSH key
  services.openssh.settings.PermitRootLogin = "prohibit-password";

  # Lockout guard (implementation-plan.md #20): root is locked above, and
  # NixOS' own lockout assertion only fires when users.mutableUsers = false.
  # With mutableUsers = true (luxos' default) a host whose selection forgets
  # every ./users/<name> import would otherwise build and lock you out. This
  # is an existence check only — a wheel user or a root SSH key must be
  # declared — never a password check: mutable passwords live in /etc/shadow
  # (set with `passwd`), invisible to the build, so checking them would
  # reject perfectly valid setups.
  assertions = [
    {
      assertion = !config.users.mutableUsers || (
        lib.any (u: lib.elem "wheel" u.extraGroups) (lib.attrValues config.users.users)
        || (config.users.users.root.openssh.authorizedKeys.keys or []) != []
      );
      message = ''
        luxos lockout guard: users.mutableUsers is true, but no declared user has
        "wheel" in extraGroups and root has no SSH authorized key. Building this
        would lock you out. Select a user module (modules/users/<name>) with a
        wheel user, give root an SSH key, or set users.allowNoPasswordLogin = true
        to bypass (NixOS' own escape hatch).
      '';
    }
  ];

  #──[Services]──────────────────────────────────────────────────────────────

  services.openssh.enable = true;
  services.atd.enable = true;
  services.cron.enable = true;

  # Strict rpfilter drops a full-tunnel VPN's own encrypted replies once the
  # default route moves onto the tunnel (Tailscale exit node, wg-quick 0.0.0.0/0).
  networking.firewall.checkReversePath = lib.mkDefault "loose";

  #──[System]────────────────────────────────────────────────────────────────

  # tmpfs for /tmp - clears on reboot (modern standard)
  boot.tmp = {
    useTmpfs = true;
    tmpfsSize = "50%";  # limit to 50% of RAM
  };

  zramSwap.enable = true; # 50% RAM compressed swap, no disk needed

  # A frozen kernel leaves no logs; panicking is what gets a crash into pstore.
  boot.kernel.sysctl = {
    "kernel.hardlockup_panic" = 1;
    "kernel.panic" = 20;
  };

  systemd.settings.Manager.RuntimeWatchdogSec = "30s";

  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  nix.registry.nixpkgs.flake = inputs.nixpkgs;
  nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];

  # Self-cleanup: prune old generations weekly, dedupe the store.
  nix.gc = {
    automatic = true;
    dates = "weekly";
    options = "--delete-older-than 14d";
  };
  nix.optimise.automatic = true;
  nix.settings.auto-optimise-store = true;
}
