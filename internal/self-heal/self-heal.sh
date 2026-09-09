#!/usr/bin/env bash
set -euo pipefail

# Self-healing sequence: rebuilds the pools, regenerates configuration.nix,
# then heals /etc/nixos and CONFIG_DIR's root symlinks to match it.
# Pure sequence — no flag parsing, no hostname resolution, no knowledge of the
# real nixos-rebuild binary. That's rebuild's job.
# Callers must source internal/env.sh first.

SELF_HEAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SELF_HEAL_DIR/../links/links.sh"
source "$SELF_HEAL_DIR/../configgen/configgen.sh"
source "$SELF_HEAL_DIR/../staging/staging.sh"

self_heal::run() {
    local machine_dir="$1"

    # flake.nix must exist and list every machine before staging copies it
    echo "Generating flake.nix from channels.toml..."
    configgen::generate_flake

    # Rebuild the users/services/modules pools before anything reads them
    links::build_all_pools

    echo "Generating configuration.nix from machine.toml..."
    configgen::generate "$machine_dir" || {
        echo "Error: Failed to generate configuration.nix" >&2
        exit 1
    }

    echo "Generating default.nix from local/..."
    configgen::generate_default "$machine_dir" || {
        echo "Error: Failed to generate default.nix" >&2
        exit 1
    }

    # Materialize the flake root, picking up the configuration.nix just
    # generated along with everything else it imports
    echo "Materializing $STAGING_DIR from machine.toml..."
    staging::materialize "$machine_dir"

    links::ensure_etc_nixos

    # Auto-heal CONFIG_DIR root symlinks: machine files + machine.toml
    links::link_machine_files "$(basename "$machine_dir")"
}
