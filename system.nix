{ pkgs, lib, inputs, ... }:

{
  imports = [
    ../hardware-configuration.nix  # Auto-generated filesystems
  ];

  #──[Packages]──────────────────────────────────────────────────────────────

  nixpkgs.config.allowUnfree = true;

  environment.systemPackages = with pkgs; [
    # Self-healing nixos-rebuild wrapper. The store copy is only a
    # bootstrap: bin/nixos-rebuild resolves its own tree from BASH_SOURCE,
    # which points into /run/current-system/sw here, so it can't be
    # embedded verbatim. Embedding internal/env.sh instead keeps
    # FRAMEWORK_DIR defined in exactly one place and hands off to the
    # real entrypoint in the framework repo.
    (pkgs.writeShellScriptBin "nixos-rebuild" ''
      ${builtins.readFile ./internal/env.sh}

      if [[ ! -x "$FRAMEWORK_DIR/bin/nixos-rebuild" ]]; then
          echo "Error: luxos framework not found at $FRAMEWORK_DIR" >&2
          echo "Clone it there, or run the real nixos-rebuild directly." >&2
          exit 1
      fi

      exec "$FRAMEWORK_DIR/bin/nixos-rebuild" "$@"
    '')
    micro
    ncdu
    tailscale
    ranger
    zoxide
    starship
    btop
    gh
    cmatrix
    fastfetch

    sshfs
    mosh
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

    go
    python3
    nodejs
    cargo
    rustc
    gcc
    # claude-code

    kitty.terminfo
  ];

  programs.nix-ld.enable = true;

  #──[Users]─────────────────────────────────────────────────────────────────

  users.users.root.hashedPassword = "!"; # No password login for root; use sudo or SSH key
  services.openssh.settings.PermitRootLogin = "prohibit-password";

  #──[Services]──────────────────────────────────────────────────────────────

  services.openssh.enable = true;
  services.atd.enable = true;
  services.cron.enable = true;
  virtualisation.docker.enable = true;
  virtualisation.docker.package = pkgs.docker_29;

  # Firewall configuration
  networking.firewall.allowedUDPPortRanges = [
    { from = 60000; to = 61000; }  # mosh
  ];

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
