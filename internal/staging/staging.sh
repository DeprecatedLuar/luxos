#!/usr/bin/env bash
set -euo pipefail

# Materializes the active machine's flake root at $STAGING_DIR: an exact
# mirror of both repos as real files, since a flake evaluates a copy of its
# own directory and can't reach outside it. Structure is preserved verbatim,
# so any relative import that resolves in the source tree resolves here too.
# Callers must source internal/env.sh first.

STAGING_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STAGING_LIB_DIR/../configgen/configgen.sh"

# Refuse to touch $STAGING_DIR unless it's empty or one we generated
# ourselves — same discipline as links::build_pool's real-file guard.
_staging_guard() {
    if [[ -e "$STAGING_DIR" && ! -f "$STAGING_DIR/.luxos-staging" ]]; then
        echo "Error: $STAGING_DIR exists but isn't a luxos-managed staging tree" >&2
        echo "  Remove it manually first if that's intended." >&2
        exit 1
    fi
}

staging::materialize() {
    local machine_dir="$1"

    _staging_guard

    sudo rm -rf "$STAGING_DIR"
    sudo mkdir -p "$STAGING_DIR/config"
    sudo touch "$STAGING_DIR/.luxos-staging"

    sudo cp -r "$FRAMEWORK_DIR/." "$STAGING_DIR/framework"
    sudo rm -rf "$STAGING_DIR/framework/.git"

    # Machines only — the pools are a bash-side name index of symlinks into
    # these same directories, and absolute symlinks can't survive the copy.
    sudo cp -r "$CONFIG_DIR/.system/machines" "$STAGING_DIR/config/machines"

    sudo cp /etc/nixos/hardware-configuration.nix "$STAGING_DIR/hardware-configuration.nix"

    if [[ ! -f "$CONFIG_DIR/flake.nix" ]]; then
        echo "Error: $CONFIG_DIR/flake.nix not found — run 'setup' first" >&2
        exit 1
    fi
    sudo cp "$CONFIG_DIR/flake.nix" "$STAGING_DIR/flake.nix"
    [[ -f "$CONFIG_DIR/flake.lock" ]] && sudo cp "$CONFIG_DIR/flake.lock" "$STAGING_DIR/flake.lock"

    sudo cp "$machine_dir/configuration.nix" "$STAGING_DIR/configuration.nix"
}
