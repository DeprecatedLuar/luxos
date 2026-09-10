#!/usr/bin/env bash
set -euo pipefail

# Self-healing sequence: rebuilds the active machine's union folders,
# regenerates every default.nix, materializes staging, generates flake.nix
# and configuration.nix straight into it, then heals /etc/nixos to match.
# Pure sequence — no flag parsing, no hostname resolution, no knowledge of the
# real nixos-rebuild binary. That's rebuild's job. This is also the one place
# that discovers peer machines and loops the declared kinds — links.sh and
# configgen.sh both stay pure black boxes: given a kind and a peer list they
# place files; they don't decide what those are.
# Callers must source internal/env.sh first.

SELF_HEAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SELF_HEAL_DIR/../links/links.sh"
source "$SELF_HEAL_DIR/../configgen/configgen.sh"
source "$SELF_HEAL_DIR/../staging/staging.sh"

# Every other machine directory at CONFIG_DIR root, sorted, excluding the one
# being healed. Union kinds (users/modules/services) fold each of these in.
_self_heal_peer_dirs() {
    local machine_name="$1"
    local name
    for name in $(configgen::discover_machines); do
        [[ "$name" == "$machine_name" ]] && continue
        echo "$CONFIG_DIR/$name"
    done
}

self_heal::run() {
    local machine_dir="$1"
    local machine_toml="$machine_dir/machine.toml"
    local machine_name
    machine_name=$(basename "$machine_dir")

    configgen::generate_root_gitignore

    local -a peer_dirs=()
    mapfile -t peer_dirs < <(_self_heal_peer_dirs "$machine_name")

    # Build the union before anything resolves names against it, then check
    # every kind's names are unique before generating anything from them —
    # a collision between a peer's file and the framework's is exactly the
    # silent-wrong-file case this catches (implementation-plan.md §3).
    local kind
    for kind in "${CONFIGGEN_KINDS[@]}"; do
        echo "Building union folder $kind for $machine_name..."
        links::build_union "$machine_dir" "$kind" ${peer_dirs[@]+"${peer_dirs[@]}"}
        configgen::validate_unique_names "$machine_dir" "$kind"
    done

    # Regenerate each kind's default.nix from machine.toml's declared names
    local -a declared
    for kind in "${CONFIGGEN_KINDS[@]}"; do
        echo "Generating $kind/default.nix from machine.toml..."
        mapfile -t declared < <(configgen::declared_names "$machine_toml" "$kind")
        configgen::generate_folder_default "$machine_dir" "$kind" ${declared[@]+"${declared[@]}"}
    done

    # local/ has no declared names — always discovery mode
    echo "Generating local/default.nix from local/..."
    configgen::generate_default "$machine_dir"

    # Materialize the flake root, picking up every default.nix just
    # generated along with everything else the union pulled in
    echo "Materializing $STAGING_DIR from $machine_dir..."
    staging::materialize "$machine_dir"

    # flake.nix and configuration.nix are written last: their imports
    # (./configuration.nix, ./framework/..., ./config/...) only resolve at
    # $STAGING_DIR, which materialize has just built. Generated unprivileged
    # to a temp file, then installed by staging (the one place that sudo
    # cps into $STAGING_DIR).
    local tmp_flake tmp_config
    tmp_flake=$(mktemp)
    echo "Generating flake.nix from channels.toml..."
    configgen::generate_flake "$machine_name" "$tmp_flake"
    staging::install_generated "$tmp_flake" "flake.nix"
    rm -f "$tmp_flake"

    tmp_config=$(mktemp)
    echo "Generating configuration.nix from machine.toml..."
    configgen::generate "$machine_dir" "$tmp_config"
    staging::install_generated "$tmp_config" "configuration.nix"
    rm -f "$tmp_config"

    links::ensure_etc_nixos
}
