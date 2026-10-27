# Builds the luxos binary. Hosts consume packages.<system>.default as the
# `luxos` flake input that luxos adds to every host's flake.nix.
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";

  outputs = { self, nixpkgs, ... }:
    let
      system = "x86_64-linux";
      version = "0.1.0";
      vendorHash = "sha256-TyLw+w0sa/p8fCg15QlqhK34Y70oc2M/ZD/4uLSuuC4=";

      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      packages.${system}.default = pkgs.buildGoModule {
        pname = "luxos";
        inherit version vendorHash;
        src = ./.;
        subPackages = [ "cmd/luxos" ];
        ldflags = [
          "-X github.com/DeprecatedLuar/luxos/internal/commands.revision=${self.rev or self.dirtyRev or "unknown"}"
          "-X github.com/DeprecatedLuar/luxos/internal/password.ageBin=${pkgs.age}/bin/age"
          "-X github.com/DeprecatedLuar/luxos/internal/password.ageKeygenBin=${pkgs.age}/bin/age-keygen"
          "-X github.com/DeprecatedLuar/luxos/internal/password.mkpasswdBin=${pkgs.mkpasswd}/bin/mkpasswd"
          "-X github.com/DeprecatedLuar/luxos/internal/shell.bundledPath=${pkgs.gitMinimal}/bin"
        ];

        # `lux` is a second name for the same binary, so every host that
        # installs this package gets both commands on PATH.
        postInstall = ''
          ln -s luxos $out/bin/lux
        '';

        meta.mainProgram = "luxos";
      };
    };
}
