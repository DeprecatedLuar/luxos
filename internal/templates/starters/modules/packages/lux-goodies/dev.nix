{ pkgs, ... }:

{
  environment.systemPackages = with pkgs; [
    go go-tools gotools golangci-lint
    python3
    nodejs
    cargo
    rustc
    gcc
    gh

    # PyGObject
    gtk3
    gobject-introspection
    (python3.withPackages (ps: with ps; [ pygobject3 ]))
  ];

  # GObject-introspection typelibs are not linked into the system profile by
  # default; PyGObject needs both the link and the path to resolve namespaces.
  environment.pathsToLink = [ "/lib/girepository-1.0" ];
  environment.sessionVariables.GI_TYPELIB_PATH = "/run/current-system/sw/lib/girepository-1.0";
}
