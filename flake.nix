# Builds the luxos binary. Hosts consume packages.<system>.default as the
# `luxos` flake input declared by the framework's flake-file bootstrap.
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";

  outputs = { self, nixpkgs, ... }:
    let
      system = "x86_64-linux";
      version = "0.1.0";
      vendorHash = "sha256-gicpSIy1YB5046OK0FkB20+HFG1Mn98bN4fSr2HdLtw=";

      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "luxos";
        inherit version vendorHash;
        src = ./.;
        subPackages = [ "cmd/luxos" ];
        ldflags = [ "-X github.com/DeprecatedLuar/luxos/internal/commands.revision=${self.rev or self.dirtyRev or "unknown"}" ];

        # `lux` is a second name for the same binary, so every host that
        # installs this package gets both commands on PATH.
        postInstall = ''
          ln -s luxos $out/bin/lux
        '';

        meta.mainProgram = "luxos";
      };
    };
}
