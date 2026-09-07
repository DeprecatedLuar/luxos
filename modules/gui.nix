{ config, pkgs, lib, mainUser, compositors, ... }:

let
  # Helper flags for conditional logic
  hasHyprland = builtins.elem "hyprland" compositors;
  hasNiri = builtins.elem "niri" compositors;
  hasXfce = builtins.elem "xfce" compositors;
  hasOpenbox = builtins.elem "openbox" compositors;
  hasI3 = builtins.elem "i3" compositors;
in
{
  #──[GUI Packages]──────────────────────────────────────────────────────────

  environment.systemPackages = with pkgs; [
    # Compositor-agnostic GUI apps
    kitty
    firefox
    vscode-fhs
    rofi
    feh
    imagemagick
    pavucontrol
    libnotify
    brightnessctl
    blueman
    swaybg
    xorg.xinit
    xorg.libXext
    xorg.libX11
    xorg.libXrender
    xorg.libXtst
    xorg.libXi
    celluloid
    grimblast
    adwaita-icon-theme
    adw-gtk3    
    zathura
    wl-clipboard
    # Multi-MIME clipboard client deps: wl-copy can only advertise a single
    # MIME type per offer, so file copies need a GTK client that advertises
    # text/uri-list and x-special/gnome-copied-files together (like PCManFM).
    gtk3
    gobject-introspection
    (python3.withPackages (ps: with ps; [ pygobject3 ]))
    wtype
    playerctl
    xfce.tumbler
    ffmpegthumbnailer

    i3
    picom

    # Qt theming - active theme managed via dotfiles (~/.config/qt6ct/)
    qt6Packages.qt6ct
    darkly
    papirus-icon-theme
    kdePackages.breeze
    adwaita-qt6
  ]
  # Hyprland-specific packages
  ++ lib.optionals hasHyprland [
    hyprsunset
    grimblast
    hypridle
    hyprpicker
    hyprlandPlugins.hyprscrolling
    hyprlandPlugins.hyprsplit
    #hyprmon
    swayimg
  ];

  # GObject-introspection typelibs are not linked into the system profile by
  # default; PyGObject needs both the link and the path to resolve namespaces.
  environment.pathsToLink = [ "/lib/girepository-1.0" ];
  environment.sessionVariables.GI_TYPELIB_PATH = "/run/current-system/sw/lib/girepository-1.0";

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

  #──[Desktop Environment]───────────────────────────────────────────────────

  services.xserver = {
    enable = true;
    displayManager.startx.enable = true;  # Proper startx support with module paths
    desktopManager.xfce.enable = hasXfce;
    windowManager.openbox.enable = hasOpenbox;
    windowManager.i3.enable = hasI3;
  };

  services.displayManager.ly.enable = true;

  services.libinput.enable = true;  # X11 input driver (keyboard/mouse)

  programs.niri.enable = hasNiri;
  programs.hyprland.enable = hasHyprland;

  #──[Virtual Camera Support]───────────────────────────────────────────────

  boot.extraModulePackages = with pkgs; [ linuxPackages.v4l2loopback ];
  boot.kernelModules = [ "v4l2loopback" ];
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
