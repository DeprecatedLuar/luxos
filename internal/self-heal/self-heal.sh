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

self_heal::run() {
    local machine_dir="$1"

    # Rebuild the users/services/modules pools before anything reads them
    links::build_all_pools

    # Auto-generate configuration.nix from machine.toml
    echo "Generating configuration.nix from machine.toml..."
    configgen::generate "$machine_dir" || {
        echo "Error: Failed to generate configuration.nix" >&2
        exit 1
    }

    links::ensure_etc_nixos "$machine_dir/configuration.nix"

    # Auto-heal CONFIG_DIR root symlinks: machine files + machine.toml
    links::link_machine_files "$(basename "$machine_dir")"
}
