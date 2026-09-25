# LUXOS property - keep walking buddy
{ pkgs, lib, inputs, config, ... }:

# Foundation, not policy: every setting here is a lib.mkDefault so a host module
# overrides it with a plain assignment. What stays undefaulted is what breaks the
# system if changed: list options that must merge (systemPackages carries luxos,
# experimental-features carries flakes), the flake-pinned registry/nixPath, the
# lockout assertion, and imports/etc plumbing.

{
  imports = [ ../hardware-configuration.nix ../boot.nix ./environment.nix ./gpu.nix ./luxos-hardware.nix ./luxos-hardware-defaults.nix ];

  #──[Packages]──────────────────────────────────────────────────────────────

  nixpkgs.config.allowUnfree = lib.mkDefault true;
  environment.systemPackages = with pkgs; [
    ncdu
    fastfetch

    nano
    vim
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
  ] ++ [
    # The luxos binary itself, from the flake input declared in flake-file.nix.
    inputs.luxos.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  programs.nix-ld.enable = lib.mkDefault true;

  # Put personal bin directories on PATH.
  environment.localBinInPath = lib.mkDefault true;
  environment.homeBinInPath = lib.mkDefault true;

  # The active host's selection, baked into the generation so `luxos module
  # list` can tell enabled-and-running from enabled-but-staged. Deliberately
  # not an environment.etc entry: nothing ever reads it from /etc.
  system.extraSystemBuilderCmds = ''
    mkdir -p $out/luxos
    cp ${../config/modules/default.nix} $out/luxos/modules.nix
  '';

  #──[Users]─────────────────────────────────────────────────────────────────

  users.users.root.hashedPassword = lib.mkDefault "!"; # No password login for root; use sudo or SSH key
  services.openssh.settings.PermitRootLogin = lib.mkDefault "prohibit-password";

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

  networking.networkmanager.enable = lib.mkDefault true;

  services.openssh.enable = lib.mkDefault true;
  services.atd.enable = lib.mkDefault true;
  services.cron.enable = lib.mkDefault true;

  # Strict rpfilter drops a full-tunnel VPN's own encrypted replies once the
  # default route moves onto the tunnel (Tailscale exit node, wg-quick 0.0.0.0/0).
  networking.firewall.checkReversePath = lib.mkDefault "loose";

  #──[System]────────────────────────────────────────────────────────────────

  # tmpfs for /tmp - clears on reboot (modern standard)
  boot.tmp = {
    useTmpfs = lib.mkDefault true;
    tmpfsSize = lib.mkDefault "50%";  # limit to 50% of RAM
  };

  zramSwap.enable = lib.mkDefault true; # 50% RAM compressed swap, no disk needed

  # A frozen kernel leaves no logs; panicking is what gets a crash into pstore.
  boot.kernel.sysctl = {
    "kernel.hardlockup_panic" = lib.mkDefault 1;
    "kernel.panic_on_oops" = lib.mkDefault 1;
    "kernel.panic" = lib.mkDefault 10;
  };

  systemd.settings.Manager.RuntimeWatchdogSec = lib.mkDefault "30s";

  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  nix.registry.nixpkgs.flake = inputs.nixpkgs;
  nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];

  nix.optimise.automatic = lib.mkDefault true;
  nix.settings.auto-optimise-store = lib.mkDefault true;
}
