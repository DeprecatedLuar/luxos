{ config, pkgs, lib, mainUser, hostName, compositors, ... }:

let
  hasDesktop = compositors != [];
in
{
  imports = [
    /etc/nixos/hardware-configuration.nix  # Auto-generated filesystems
  ];

     #──[Packages]──────────────────────────────────────────────────────────────

       nixpkgs.config.allowUnfree = true;

       environment.systemPackages = with pkgs; [
         # Self-healing nixos-rebuild wrapper
          (pkgs.writeShellScriptBin "nixos-rebuild" (builtins.readFile ./bin/nixos-rebuild))
         # Dead man's switch for rebuilds
          (pkgs.writeShellScriptBin "meltdown" (builtins.readFile ./scripts/lib/meltdown))

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

       ] ++ lib.optionals hasDesktop [
         xdotool
         ydotool
         evtest
         mpv
       ];

       programs.nix-ld.enable = true;

     #──[Audio & Bluetooth]─────────────────────────────────────────────────────

       security.rtkit.enable = lib.mkIf hasDesktop true;

       services.pulseaudio.enable = lib.mkIf hasDesktop false;

       services.pipewire = lib.mkIf hasDesktop {
         enable = true;
         alsa.enable = true;
         alsa.support32Bit = true;
         pulse.enable = true;
         wireplumber.extraConfig.bluetoothEnhancements = {
           "monitor.bluez.properties" = {
             "bluez5.enable-sbc-xq" = true;
             "bluez5.enable-msbc" = true;
             "bluez5.enable-hw-volume" = true;
             "bluez5.roles" = [ "a2dp_sink" "a2dp_source" "hsp_hs" "hsp_ag" "hfp_hf" "hfp_ag" ];
           };
         };
       };

       hardware.bluetooth = lib.mkIf hasDesktop {
         enable = true;
         powerOnBoot = true;
         settings.General.Experimental = true;
       };

     #──[Users]─────────────────────────────────────────────────────────────────

       users.users.root.hashedPassword = "!"; # No password login for root; use sudo or SSH key
       services.openssh.settings.PermitRootLogin = "prohibit-password";

     #──[Input Devices]─────────────────────────────────────────────────────────

       boot.kernelModules = lib.optionals hasDesktop [ "uinput" ];
       hardware.uinput.enable = lib.mkIf hasDesktop true;

     #──[Services]──────────────────────────────────────────────────────────────

       services.upower.enable = lib.mkIf hasDesktop true;
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

      systemd.services = { };

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

       # Self-cleanup: prune old generations weekly, dedupe the store.
       nix.gc = {
         automatic = true;
         dates = "weekly";
         options = "--delete-older-than 14d";
       };
       nix.optimise.automatic = true;
       nix.settings.auto-optimise-store = true;

     }
