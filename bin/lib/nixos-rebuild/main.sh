#!/usr/bin/env bash
set -euo pipefail

# The self-healing nixos-rebuild wrapper's entrypoint logic: flag parsing,
# resolving this machine's directory, delegating to self-heal, then exec'ing
# the real nixos-rebuild binary.
# Callers must source internal/env.sh first.

source "$FRAMEWORK_DIR/internal/self-heal/self-heal.sh"

rebuild::run() {
    # Parse flags
    local args=()
    local bypass=false
    local meltdown=false
    local arg
    for arg; do
        if [[ "$arg" == "--bypass" ]]; then
            bypass=true
        elif [[ "$arg" == "--meltdown" ]]; then
            meltdown=true
        else
            args+=("$arg")
        fi
    done

    local rebuild_bin
    rebuild_bin=$(nix-build '<nixpkgs/nixos>' -A config.system.build.nixos-rebuild --no-out-link)/bin/nixos-rebuild

    if $bypass; then
        exec "$rebuild_bin" "${args[@]}"
    fi

    local hostname machines_dir machine_dir
    hostname=$(hostname)
    machines_dir="$CONFIG_DIR/.system/machines"
    machine_dir="$machines_dir/$hostname"

    # Check if machine config exists
    if [[ ! -d "$machine_dir" ]]; then
        echo "Error: Machine config not found: $machine_dir" >&2
        exit 1
    fi

    self_heal::run "$machine_dir"

    if $meltdown; then
        exec meltdown "$rebuild_bin" "${args[@]}"
    else
        exec "$rebuild_bin" "${args[@]}"
    fi
}
