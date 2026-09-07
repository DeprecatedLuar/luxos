#!/usr/bin/env bash
# Shared functions for nixos-rebuild.sh and setup.sh.
# Callers must set FRAMEWORK_DIR and CONFIG_DIR before sourcing this file.

#──[Config parsing]────────────────────────────────────────────────────────────

# Parse mainUser out of a generated configuration.nix
parse_main_user() {
    local config_file="$1"
    awk '/mainUser = / {match($0, /"([^"]+)"/, arr); print arr[1]}' "$config_file"
}

#──[/etc/nixos healing]────────────────────────────────────────────────────────

# Ensure /etc/nixos is a real directory with only configuration.nix symlinked
# in. Idempotent; self-heals a stale symlink from the old single-file scheme.
ensure_etc_nixos() {
    local machine_config="$1"

    if [[ -L "/etc/nixos" ]]; then
        echo "Converting /etc/nixos from symlink to real directory..."
        sudo rm -f /etc/nixos
    fi

    if [[ ! -d "/etc/nixos" ]]; then
        echo "Creating /etc/nixos directory..."
        sudo mkdir -p /etc/nixos
    fi

    local link="/etc/nixos/configuration.nix"
    if [[ ! -L "$link" ]] || [[ "$(readlink -f "$link")" != "$machine_config" ]]; then
        echo "Symlinking /etc/nixos/configuration.nix -> $machine_config"
        sudo rm -f "$link"
        sudo ln -sf "$machine_config" "$link"
    fi

    # For service environmentFiles that expect it to exist even when empty
    if [[ ! -f "/etc/nixos/env" ]]; then
        echo "Creating /etc/nixos/env..."
        sudo touch /etc/nixos/env
        sudo chmod 600 /etc/nixos/env
    fi
}

#──[CONFIG_DIR root symlinks]──────────────────────────────────────────────────

# Symlink a machine's editable *.nix files (excluding generated/internal ones)
# plus machine.toml into CONFIG_DIR's root, for `micro $CONFIG_DIR/machine.toml`
# style convenience editing. Idempotent.
link_machine_files() {
    local machine="$1"
    local machine_dir="$CONFIG_DIR/machines/$machine"

    local file filename target link
    for file in "$machine_dir"/*.nix; do
        [[ -f "$file" ]] || continue
        filename=$(basename "$file")
        if [[ "$filename" != "default.nix" && "$filename" != "services.nix" && "$filename" != "configuration.nix" ]]; then
            target="machines/$machine/$filename"
            link="$CONFIG_DIR/$filename"
            if [[ ! -L "$link" ]] || [[ "$(readlink "$link")" != "$target" ]]; then
                rm -f "$link"
                ln -s "$target" "$link"
            fi
        fi
    done

    if [[ -f "$machine_dir/machine.toml" ]]; then
        target="machines/$machine/machine.toml"
        link="$CONFIG_DIR/machine.toml"
        if [[ ! -L "$link" ]] || [[ "$(readlink "$link")" != "$target" ]]; then
            rm -f "$link"
            ln -s "$target" "$link"
        fi
    fi
}

#──[Discovery]──────────────────────────────────────────────────────────────

# List available module basenames under $FRAMEWORK_DIR/modules (e.g. "gaming gui")
discover_modules() {
    local f names=""
    for f in "$FRAMEWORK_DIR"/modules/*.nix; do
        [[ -e "$f" ]] || continue
        names="$names $(basename "$f" .nix)"
    done
    echo "${names# }"
}
