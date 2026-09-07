{ lib, ... }:

let
  # configDir can't be threaded in via _module.args: it's consumed below to
  # compute this module's own `imports`, and _module.args values require a
  # config fixpoint that isn't available yet at import-resolution time (causes
  # "infinite recursion encountered"). LUXOS_CONFIG_DIR is exported by
  # nixos-rebuild.sh before invoking the real nixos-rebuild binary.
  configDir =
    let v = builtins.getEnv "LUXOS_CONFIG_DIR"; in
    if v != "" then v
    else throw "service-loader.nix: LUXOS_CONFIG_DIR is not set in the environment";

  # Read services config if it exists
  servicesFile = "${configDir}/services/services.nix";
  enabledServices = if builtins.pathExists servicesFile
    then (import servicesFile { }).enabledServices
    else [ ];

  # Map service names to their file paths
  serviceImports = map (name: "${configDir}/services/available/${name}.nix") enabledServices;

in
{
  imports = serviceImports;
}
