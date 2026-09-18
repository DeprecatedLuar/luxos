# The channel overlay (implementation-plan.md F5): an input is a channel
# iff it exposes legacyPackages for this system, and each channel is
# exposed as pkgs.<input-name>.<package>. No list of channels is
# maintained anywhere - this is computed from the flake's own inputs.
{ lib, inputs, system }:
let
  channels = lib.filterAttrs (n: v: v ? legacyPackages.${system}) inputs;
in
final: prev:
lib.mapAttrs
  (name: input:
    # nixpkgs (the base channel, F4) aliases prev - pkgs already is that
    # channel; every other channel is freshly imported so it gets its own
    # instantiation, inheriting prev's config (allowUnfree and friends).
    if name == "nixpkgs" then
      prev
    else
      import input { inherit system; inherit (prev) config; }
  )
  channels
