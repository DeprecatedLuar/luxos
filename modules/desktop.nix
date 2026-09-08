{ pkgs, ... }:

{
  #──[X11 compatibility]─────────────────────────────────────────────────────
  # XWayland (enabled by default under programs.hyprland) needs these even
  # on a Wayland-only machine.

  environment.systemPackages = with pkgs; [
    xorg.libXext
    xorg.libX11
    xorg.libXrender
    xorg.libXtst
    xorg.libXi
  ];

  services.flatpak.enable = true;
  xdg.portal.enable = true;  # Required for Flatpak and desktop integration
  xdg.portal.extraPortals = [ pkgs.xdg-desktop-portal-gtk ];

  services.gvfs.enable = true;
  # binfmt lets `./foo.AppImage` run directly; libxshmfence is missing from
  # appimage-run's FHS env and required by Electron AppImages.
  programs.appimage = {
    enable = true;
    binfmt = true;
    package = pkgs.appimage-run.override {
      extraPkgs = pkgs: with pkgs; [ libxshmfence zstd ];
    };
  };
  programs.nix-ld.enable = true;
  # Portable (non-AppImage) binaries in ~/Workspace/tools/ need these to
  # resolve their dynamic libs against nix-ld's shimmed ld.so.
  programs.nix-ld.libraries = with pkgs; [
    xorg.libxcb
    xorg.libX11
    xorg.libXrandr
    xorg.libXi
    xorg.libXcursor
    xorg.libXdamage
    xorg.libXcomposite
    xorg.libXfixes
    xorg.libXext
    gtk3
    webkitgtk_4_1
    libsoup_3
    glib
    cairo
    pango
    gdk-pixbuf
    at-spi2-core
    dbus
    mesa
    libglvnd
    alsa-lib
    nss
    nspr
    cups
    libnotify
    libayatana-appindicator
  ];

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

  #──[Virtual Camera Support]───────────────────────────────────────────────

  boot.extraModulePackages = with pkgs; [ linuxPackages.v4l2loopback ];
  boot.kernelModules = [ "v4l2loopback" "uinput" ];
  boot.extraModprobeConfig = ''
    options v4l2loopback devices=1 video_nr=10 card_label="Virtual Camera" exclusive_caps=1
  '';

  #──[Fonts]────────────────────────────────────────────────────────────────

  fonts.packages = with pkgs; [
    noto-fonts
    noto-fonts-cjk-sans
    noto-fonts-color-emoji
    nerd-fonts.symbols-only
  ];
}
