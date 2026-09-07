#!/usr/bin/env bash
set -euo pipefail

# Self-healing nixos-rebuild wrapper
# Ensures /etc/nixos and CONFIG_DIR root-level symlinks are properly configured

FRAMEWORK_DIR="$HOME/Workspace/dev/luxos"
CONFIG_DIR="$HOME/.config/luxos"

source "$FRAMEWORK_DIR/scripts/lib/common.sh"

# Parse flags
args=()
bypass=false
meltdown=false
for arg; do
    if [[ "$arg" == "--bypass" ]]; then
        bypass=true
    elif [[ "$arg" == "--meltdown" ]]; then
        meltdown=true
    else
        args+=("$arg")
    fi
done

if $bypass; then
    rebuild_bin=$(nix-build '<nixpkgs/nixos>' -A config.system.build.nixos-rebuild --no-out-link)/bin/nixos-rebuild
    exec "$rebuild_bin" "${args[@]}"
fi

HOSTNAME=$(hostname)
MACHINES_DIR="$CONFIG_DIR/machines"
MACHINE_DIR="$MACHINES_DIR/$HOSTNAME"

# Check if machine config exists
if [[ ! -d "$MACHINE_DIR" ]]; then
    echo "Error: Machine config not found: $MACHINE_DIR" >&2
    exit 1
fi

# Auto-generate configuration.nix from machine.toml
GENERATOR="$FRAMEWORK_DIR/scripts/generate-config.sh"
if [[ -f "$GENERATOR" && -f "$MACHINE_DIR/machine.toml" ]]; then
    echo "Generating configuration.nix from machine.toml..."
    "$GENERATOR" "$MACHINE_DIR" || {
        echo "Error: Failed to generate configuration.nix" >&2
        exit 1
    }
fi

MAIN_USER=$(parse_main_user "$MACHINE_DIR/configuration.nix")
if [[ -z "$MAIN_USER" ]]; then
    echo "Error: Could not parse mainUser from configuration.nix" >&2
    exit 1
fi

ensure_etc_nixos "$MACHINE_DIR/configuration.nix"

# Auto-heal CONFIG_DIR root symlinks: machine files + machine.toml
link_machine_files "$HOSTNAME"

# Service symlink-farm: Self-healing service management
SERVICES_DIR="$CONFIG_DIR/services"
SERVICES_AVAILABLE="$SERVICES_DIR/available"
MACHINE_SERVICES_DIR="$MACHINE_DIR/services"
MACHINE_SERVICES_CONFIG="$MACHINE_DIR/services.nix"

# Ensure directories exist
mkdir -p "$SERVICES_AVAILABLE"

# Clean broken symlinks in services/available/
find "$SERVICES_AVAILABLE" -type l ! -exec test -e {} \; -delete 2>/dev/null || true

# Symlink machine services.nix -> services/services.nix
if [[ -f "$MACHINE_SERVICES_CONFIG" ]]; then
    SERVICES_CONFIG_LINK="$SERVICES_DIR/services.nix"
    if [[ ! -L "$SERVICES_CONFIG_LINK" ]] || [[ "$(readlink -f "$SERVICES_CONFIG_LINK")" != "$MACHINE_SERVICES_CONFIG" ]]; then
        echo "Symlinking services config: machines/$HOSTNAME/services.nix -> services/services.nix"
        rm -f "$SERVICES_CONFIG_LINK"
        ln -s "$MACHINE_SERVICES_CONFIG" "$SERVICES_CONFIG_LINK"
    fi
fi

# Symlink machine-specific services: machines/$HOSTNAME/services/*.nix -> services/available/local_*.nix
if [[ -d "$MACHINE_SERVICES_DIR" ]]; then
    for service_file in "$MACHINE_SERVICES_DIR"/*.nix; do
        if [[ -f "$service_file" ]]; then
            service_name=$(basename "$service_file")
            local_link="$SERVICES_AVAILABLE/local_$service_name"
            if [[ ! -L "$local_link" ]] || [[ "$(readlink -f "$local_link")" != "$service_file" ]]; then
                echo "Symlinking machine service: $service_name -> local_$service_name"
                rm -f "$local_link"
                ln -s "$service_file" "$local_link"
            fi
        fi
    done
fi

# service-loader.nix reads this (see its header comment for why it can't
# come through _module.args instead).
export LUXOS_CONFIG_DIR="$CONFIG_DIR"

# Call the real nixos-rebuild from nixpkgs
rebuild_bin=$(nix-build '<nixpkgs/nixos>' -A config.system.build.nixos-rebuild --no-out-link)/bin/nixos-rebuild

if $meltdown; then
    exec meltdown "$rebuild_bin" "${args[@]}"
else
    exec "$rebuild_bin" "${args[@]}"
fi
