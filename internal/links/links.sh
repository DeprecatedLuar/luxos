#!/usr/bin/env bash
set -euo pipefail

# Symlink management: /etc/nixos healing, CONFIG_DIR root convenience links,
# and the generated users/services/modules pools.
# Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR) first.

#──[/etc/nixos healing]────────────────────────────────────────────────────────

# Ensure /etc/nixos is a real directory with only configuration.nix symlinked
# in. Idempotent; self-heals a stale symlink from the old single-file scheme.
links::ensure_etc_nixos() {
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
links::link_machine_files() {
    local machine="$1"
    local machine_dir="$CONFIG_DIR/.system/machines/$machine"

    local file filename target link
    for file in "$machine_dir"/*.nix; do
        [[ -f "$file" ]] || continue
        filename=$(basename "$file")
        if [[ "$filename" != "default.nix" && "$filename" != "configuration.nix" ]]; then
            target=".system/machines/$machine/$filename"
            link="$CONFIG_DIR/$filename"
            if [[ ! -L "$link" ]] || [[ "$(readlink "$link")" != "$target" ]]; then
                rm -f "$link"
                ln -s "$target" "$link"
            fi
        fi
    done

    if [[ -f "$machine_dir/machine.toml" ]]; then
        target=".system/machines/$machine/machine.toml"
        link="$CONFIG_DIR/machine.toml"
        if [[ ! -L "$link" ]] || [[ "$(readlink "$link")" != "$target" ]]; then
            rm -f "$link"
            ln -s "$target" "$link"
        fi
    fi
}

#──[Symlink pools]─────────────────────────────────────────────────────────────

LINKS_POOL_ROOT_REL=".system"

# Rebuild one pool from scratch: every *.nix matched by the source globs becomes
# a symlink in the pool, keyed by basename. A pool holds nothing but generated
# symlinks, and every name must be defined exactly once across all sources.
links::build_pool() {
    local pool_dir="$1"
    shift

    if [[ -d "$pool_dir" ]]; then
        local existing
        while IFS= read -r -d '' existing; do
            if [[ ! -L "$existing" ]]; then
                echo "Error: real file in generated pool: $existing" >&2
                echo "  Pools contain only symlinks; move it into a machine's own directory." >&2
                exit 1
            fi
        done < <(find "$pool_dir" -mindepth 1 -maxdepth 1 -print0)
        find "$pool_dir" -mindepth 1 -maxdepth 1 -delete
    else
        mkdir -p "$pool_dir"
    fi

    local -A seen=()
    local src name
    for src in "$@"; do
        [[ -f "$src" ]] || continue
        name=$(basename "$src")
        if [[ -n "${seen[$name]:-}" ]]; then
            echo "Error: duplicate '$name' in pool $pool_dir" >&2
            echo "  ${seen[$name]}" >&2
            echo "  $src" >&2
            echo "  A name must be defined once; rename one of them." >&2
            exit 1
        fi
        seen[$name]="$src"
        ln -s "$src" "$pool_dir/$name"
    done
}

# Rebuild all three pools. Users and services come from the machines that author
# them; modules additionally draw from the framework's own modules/.
links::build_all_pools() {
    local machines_dir="$CONFIG_DIR/.system/machines"
    local pool_root="$CONFIG_DIR/$LINKS_POOL_ROOT_REL"

    shopt -s nullglob
    links::build_pool "$pool_root/users" "$machines_dir"/*/users/*.nix
    links::build_pool "$pool_root/services" "$machines_dir"/*/services/*.nix
    links::build_pool "$pool_root/modules" "$FRAMEWORK_DIR"/modules/*.nix "$machines_dir"/*/modules/*.nix
    shopt -u nullglob
}
