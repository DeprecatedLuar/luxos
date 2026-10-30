{ pkgs, lib, ... }:

let
  appimage-run-base = pkgs.appimage-run.override {
    extraPkgs = pkgs: with pkgs; [ libxshmfence zstd ];
  };

  # nixpkgs' appimage-run only unpacks SquashFS payloads. uruntime AppImages
  # carry DwarFS instead, so extract those here and hand the directory to the
  # stock script (-w), which runs it inside the same FHS env.
  appimage-run = pkgs.writeShellApplication {
    name = "appimage-run";
    runtimeInputs = with pkgs; [ binutils gawk coreutils dwarfs ];
    text = ''
      readonly DWARFS_MAGIC="DWARFS"
      readonly CACHE_DIR="''${XDG_CACHE_HOME:-$HOME/.cache}/appimage-run"
      readonly BASE="${lib.getExe appimage-run-base}"

      # Options (-x, -w, -d, -h) belong to the stock script.
      if [[ $# -eq 0 || "$1" == -* ]]; then exec "$BASE" "$@"; fi

      image=$(realpath "$1")
      # Payload starts where the ELF section headers end, same as appimage-exec.sh.
      offset=$(LC_ALL=C readelf -h "$image" | awk 'NR==13{s=$5} NR==18{e=$5} NR==19{n=$5} END{print s+e*n}')
      magic=$(dd if="$image" bs=1 skip="$offset" count=''${#DWARFS_MAGIC} status=none | tr -d '\0')

      if [[ "$magic" != "$DWARFS_MAGIC" ]]; then exec "$BASE" "$@"; fi

      appdir="$CACHE_DIR/$(sha256sum "$image" | awk '{print $1}')"
      if [[ ! -x "$appdir/AppRun" ]]; then
        partial="$appdir.partial"
        rm -rf "$partial" "$appdir"
        mkdir -p "$partial"
        echo "Extracting $(basename "$image") (DwarFS) to $appdir"
        dwarfsextract -i "$image" --image-offset="$offset" -o "$partial" --log-level=error
        mv "$partial" "$appdir"
      fi

      shift
      export APPIMAGE="$image"
      exec "$BASE" -w "$appdir" "$@"
    '';
  };
in
{
  #──[X11 compatibility]─────────────────────────────────────────────────────
  # XWayland (enabled by default under programs.hyprland) needs these even
  # on a Wayland-only machine.

  environment.systemPackages = with pkgs; [
    libxext
    libx11
    libxrender
    libxtst
    libxi

    # Audio & Bluetooth GUI companions (pipewire/bluetooth enabled below)
    pavucontrol
    blueman

    # Input device tooling (hardware.uinput enabled below)
    evtest
    brightnessctl
  ];

  services.flatpak.enable = true;
  xdg.portal.enable = true;  # Required for Flatpak and desktop integration
  xdg.portal.extraPortals = [ pkgs.xdg-desktop-portal-gtk ];

  services.gvfs.enable = true;
  security.polkit.enable = true;  # auth agent for GVFS/udisks2, NetworkManager GUI, etc.
  # binfmt lets `./foo.AppImage` run directly; libxshmfence is missing from
  # appimage-run's FHS env and required by Electron AppImages.
  programs.appimage = {
    enable = true;
    binfmt = true;
    package = appimage-run;
  };

  # Prebuilt desktop binaries resolve their libraries through nix-ld: the set
  # nixpkgs gives AppImages, plus what it lacks.
  programs.nix-ld.libraries = (pkgs.appimageTools.defaultFhsEnvArgs.multiPkgs pkgs) ++ (with pkgs; [
    gtk3
    webkitgtk_4_1
    libsoup_3
    libayatana-appindicator
    libnotify
    libxshmfence
    mesa
  ]);

  #──[Audio & Bluetooth]─────────────────────────────────────────────────────

  security.rtkit.enable = true;

  services.pulseaudio.enable = false;

  services.pipewire = {
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

  hardware.bluetooth = {
    enable = true;
    powerOnBoot = true;
    settings.General.Experimental = true;
  };

  #──[Input Devices]─────────────────────────────────────────────────────────

  hardware.uinput.enable = true;

  #──[Services]──────────────────────────────────────────────────────────────

  services.upower.enable = true;
  services.fwupd.enable = true;

  # Network printers announce themselves over mDNS.
  services.printing.enable = true;
  services.avahi = {
    enable = true;
    nssmdns4 = true;
    openFirewall = true;
  };

  #──[Qt Theming]───────────────────────────────────────────────────────────
  # The qt6ct plugin also answers to "qt5ct", so one value themes Qt5 and Qt6.

  qt = {
    enable = lib.mkDefault true;
    platformTheme = lib.mkDefault "qt5ct";
  };

  #──[Fonts]────────────────────────────────────────────────────────────────

  fonts.packages = with pkgs; [
    noto-fonts
    noto-fonts-cjk-sans
    noto-fonts-color-emoji
    nerd-fonts.symbols-only
  ];
}
