# LUXOS property - keep walking buddy
{ pkgs, lib, inputs, config, ... }:

# Foundation, not policy: every setting here is a lib.mkDefault so a host module
# overrides it with a plain assignment. What stays undefaulted is what breaks the
# system if changed: list options that must merge (systemPackages carries luxos,
# experimental-features carries flakes), the flake-pinned registry/nixPath,
# and etc plumbing.

let
  # nixpkgs sets services.cron.enable itself with mkDefault (1000), so a second
  # mkDefault conflicts. Just above it, still below a plain host assignment (100).
  cronPriority = 900;
in
{
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
    # The luxos binary itself, from the flake input declared by the framework.
    inputs.luxos.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  programs.nix-ld.enable = lib.mkDefault true;

  # Put personal bin directories on PATH.
  environment.localBinInPath = lib.mkDefault true;
  environment.homeBinInPath = lib.mkDefault true;

  # The selections, baked into the generation so `luxos module list` can tell
  # enabled-and-running from enabled-but-staged. Deliberately not an
  # environment.etc entry: nothing ever reads it from /etc.
  system.systemBuilderCommands = ''
    mkdir -p $out/luxos/modules/host $out/luxos/modules/machine
    cp ${../config/modules/host/default.nix} $out/luxos/modules/host/default.nix
    cp ${../config/modules/machine/default.nix} $out/luxos/modules/machine/default.nix
  '';

  #──[Users]─────────────────────────────────────────────────────────────────

  users.users.root.hashedPassword = lib.mkDefault "!"; # No password login for root; use sudo or SSH key
  services.openssh.settings.PermitRootLogin = lib.mkDefault "prohibit-password";

  # Every login user is a declared luxos user; NixOS's own lockout assertion
  # applies with immutable users.
  users.mutableUsers = lib.mkDefault false;

  #──[Services]──────────────────────────────────────────────────────────────

  networking.networkmanager.enable = lib.mkDefault true;

  services.openssh.enable = lib.mkDefault true;
  services.atd.enable = lib.mkDefault true;
  services.cron.enable = lib.mkOverride cronPriority true;

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

  # Reboot 10s after a kernel panic instead of hanging.
  boot.kernel.sysctl."kernel.panic" = lib.mkDefault 10;

  nix.gc = {
    automatic = lib.mkDefault true;
    dates = lib.mkDefault "weekly";
    options = lib.mkDefault "--delete-older-than 14d";
  };

  nix.settings.experimental-features = [ "nix-command" "flakes" ];
  nix.registry.nixpkgs.flake = inputs.nixpkgs;
  nix.nixPath = [ "nixpkgs=${inputs.nixpkgs}" ];

  nix.optimise.automatic = lib.mkDefault true;
  nix.settings.auto-optimise-store = lib.mkDefault true;
}
