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

REBUILD_HEADER='
██╗     ██╗   ██╗██╗  ██╗ ██████╗ ███████╗
██║     ██║   ██║╚██╗██╔╝██╔═══██╗██╔════╝
██║     ██║   ██║ ╚███╔╝ ██║   ██║███████╗
██║     ██║   ██║ ██╔██╗ ██║   ██║╚════██║
███████╗╚██████╔╝██╔╝ ██╗╚██████╔╝███████║
╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝
                          made by me <3 (luar)
'

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
    printf '%s\n' "$REBUILD_HEADER"

    local -A opts=()
    local -a args=()
    flags::parse_passthrough opts args "bypass:bool update-lock:bool prune:bool machine:value" "$@"

    local bypass=false update_lock=false prune=false
    [[ -n "${opts[bypass]:-}" ]] && bypass=true
    [[ -n "${opts[update-lock]:-}" ]] && update_lock=true
    [[ -n "${opts[prune]:-}" ]] && prune=true

    if $bypass; then
        # --bypass never reads .local (channel-based escape hatch, no
        # staging, no flake), so --machine would silently do nothing —
        # hard error rather than let the flag look like it took effect.
        if [[ -n "${opts[machine]:-}" ]]; then
            echo "Error: --bypass and --machine cannot be combined — --bypass never reads .local" >&2
            exit 1
        fi
        exec "$(_rebuild_bin_from_channel)/$REBUILD_BIN_RELPATH" "${args[@]}"
    fi

    local hostname machine_dir
    hostname="${opts[machine]:-$(hostname)}"
    machine_dir=$(configgen::resolve_machine "$hostname")

    self_heal::run "$machine_dir" "$prune"

    $update_lock && staging::update_lock

    local rebuild_bin
    rebuild_bin="$(_rebuild_bin_from_flake "$hostname")/$REBUILD_BIN_RELPATH"

    local flake_args=(--flake "$STAGING_DIR#$hostname")
    $update_lock || flake_args+=(--no-write-lock-file)
    flake_args+=("${args[@]}")

    exec "$rebuild_bin" "${flake_args[@]}"
}
