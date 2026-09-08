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
    local update_lock=false
    local arg
    for arg; do
        if [[ "$arg" == "--bypass" ]]; then
            bypass=true
        elif [[ "$arg" == "--meltdown" ]]; then
            meltdown=true
        elif [[ "$arg" == "--update-lock" ]]; then
            update_lock=true
        else
            args+=("$arg")
        fi
    done

    if $bypass; then
        local rebuild_bin
        rebuild_bin=$(nix-build '<nixpkgs/nixos>' -A config.system.build.nixos-rebuild --no-out-link)/bin/nixos-rebuild
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

    if $update_lock; then
        sudo nix flake update --flake "$STAGING_DIR"
        sudo install -m 644 -o "$(id -u)" -g "$(id -g)" \
            "$STAGING_DIR/flake.lock" "$CONFIG_DIR/flake.lock"
        echo "Updated $CONFIG_DIR/flake.lock — commit it."
    fi

    local rebuild_bin
    rebuild_bin=$(nix build "$STAGING_DIR#nixosConfigurations.$hostname.config.system.build.nixos-rebuild" --no-link --print-out-paths)/bin/nixos-rebuild

    local flake_args=(--flake "$STAGING_DIR#$hostname")
    $update_lock || flake_args+=(--no-write-lock-file)
    flake_args+=("${args[@]}")

    if $meltdown; then
        exec meltdown "$rebuild_bin" "${flake_args[@]}"
    else
        exec "$rebuild_bin" "${flake_args[@]}"
    fi
}
