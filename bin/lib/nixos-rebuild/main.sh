#!/usr/bin/env bash
set -euo pipefail

# The self-healing nixos-rebuild wrapper's entrypoint logic: flag parsing,
# resolving this machine's directory, delegating to self-heal, then exec'ing
# the real nixos-rebuild binary.
# Callers must source internal/env.sh first.

# The nix attribute path to a flake's built nixos-rebuild, and the binary's
# location inside that derivation's output — one constant, used by both
# acquisition strategies below instead of being typed twice in two syntaxes.
REBUILD_ATTR="config.system.build.nixos-rebuild"
REBUILD_BIN_RELPATH="bin/nixos-rebuild"

# Channel-based (--bypass) acquisition target: the classic <nixpkgs/nixos>
# NIX_PATH entry, used only when staging itself is broken.
REBUILD_CHANNEL_EXPR='<nixpkgs/nixos>'

REBUILD_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$REBUILD_LIB_DIR/../flags.sh"
source "$FRAMEWORK_DIR/internal/self-heal/self-heal.sh"

# Escape hatch: build nixos-rebuild from the channel-based <nixpkgs/nixos>,
# bypassing staging and the flake entirely.
_rebuild_bin_from_channel() {
    nix-build "$REBUILD_CHANNEL_EXPR" -A "$REBUILD_ATTR" --no-out-link
}

# The normal path: build nixos-rebuild out of the staged flake's own locked
# nixpkgs, for the given host.
_rebuild_bin_from_flake() {
    local hostname="$1"
    nix build "$STAGING_DIR#nixosConfigurations.$hostname.$REBUILD_ATTR" \
        --no-link --print-out-paths
}

rebuild::run() {
    local -A opts=()
    local -a args=()
    flags::parse_passthrough opts args "bypass:bool meltdown:bool update-lock:bool" "$@"

    local bypass=false meltdown=false update_lock=false
    [[ -n "${opts[bypass]:-}" ]] && bypass=true
    [[ -n "${opts[meltdown]:-}" ]] && meltdown=true
    [[ -n "${opts[update-lock]:-}" ]] && update_lock=true

    if $bypass; then
        exec "$(_rebuild_bin_from_channel)/$REBUILD_BIN_RELPATH" "${args[@]}"
    fi

    local hostname machine_dir
    hostname=$(hostname)
    machine_dir=$(configgen::resolve_machine "$hostname")

    self_heal::run "$machine_dir"

    $update_lock && staging::update_lock

    local rebuild_bin
    rebuild_bin="$(_rebuild_bin_from_flake "$hostname")/$REBUILD_BIN_RELPATH"

    local flake_args=(--flake "$STAGING_DIR#$hostname")
    $update_lock || flake_args+=(--no-write-lock-file)
    flake_args+=("${args[@]}")

    if $meltdown; then
        exec meltdown "$rebuild_bin" "${flake_args[@]}"
    else
        exec "$rebuild_bin" "${flake_args[@]}"
    fi
}
