# LUXOS property - keep walking buddy
{ config, lib, pkgs, ... }:

# Each user unit points luxos.users.<name>.password at its age-encrypted hash.
# Activation decrypts it with this computer's key before NixOS writes users.
# Every normal user comes from a user unit, and users stay immutable.

let
  keysDir = "/var/lib/luxos/keys";
  runDir = "/run/luxos/passwords";
  age = "${pkgs.age}/bin/age";

  stray = lib.attrNames (lib.filterAttrs
    (n: u: u.isNormalUser && !(config.luxos.users ? ${n})) config.users.users);

  decrypt = name: user: ''
    if ${age} -d -i ${lib.escapeShellArg "${keysDir}/${name}"} -o ${lib.escapeShellArg "${runDir}/${name}"} ${user.password}; then
      chmod 0400 ${lib.escapeShellArg "${runDir}/${name}"}
    else
      rm -f ${lib.escapeShellArg "${runDir}/${name}"}
      echo ${lib.escapeShellArg "luxos: cannot decrypt the password of ${name}; its account stays locked (luxos key add ${name})"} >&2
    fi
  '';
in
{
  options.luxos.users = lib.mkOption {
    type = lib.types.attrsOf (lib.types.submodule {
      options.password = lib.mkOption {
        type = lib.types.path;
        description = "The user's yescrypt hash, age-encrypted to the user's key.";
      };
    });
    default = { };
    description = "Users whose password luxos decrypts at activation.";
  };

  config = {
    assertions = [
      {
        assertion = lib.any (u: lib.elem "wheel" u.extraGroups) (lib.attrValues config.users.users)
          || config.users.users.root.openssh.authorizedKeys.keys != [ ];
        message = "luxos: no user is in wheel, so nobody can sudo: lux user config <user>";
      }
      {
        assertion = !config.users.mutableUsers;
        message = "luxos: users.mutableUsers must stay false; users are declared by user units: luxos user add <name>";
      }
      {
        assertion = stray == [ ];
        message = "luxos: users not declared by a user unit: ${lib.concatStringsSep ", " stray}; create them with luxos user add <name>";
      }
    ];

    users.users = lib.mapAttrs (name: _: { hashedPasswordFile = "${runDir}/${name}"; }) config.luxos.users;

    system.activationScripts.luxosPasswords = {
      deps = [ ];
      text = ''
        (
          umask 077
          rm -rf ${runDir}
          mkdir -p ${runDir}
          ${lib.concatStrings (lib.mapAttrsToList decrypt config.luxos.users)}
        )
      '';
    };
    system.activationScripts.users.deps = [ "luxosPasswords" ];
  };
}
