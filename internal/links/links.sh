#!/usr/bin/env bash
set -euo pipefail

# Symlink management: /etc/nixos healing, CONFIG_DIR root convenience links,
# and the generated users/services/modules pools.
# Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR) first.

#──[/etc/nixos healing]────────────────────────────────────────────────────────

# Ensure /etc/nixos is a real directory holding only hardware-configuration.nix
# and env — configuration.nix now lives under $STAGING_DIR (see
# internal/staging/staging.sh), not here. Idempotent; self-heals a stale
# symlink from the old single-file scheme, and removes the old
# configuration.nix symlink if one is still present from before the flake
# migration.
links::ensure_etc_nixos() {
    if [[ -L "/etc/nixos" ]]; then
        echo "Converting /etc/nixos from symlink to real directory..."
        sudo rm -f /etc/nixos
    fi

    if [[ ! -d "/etc/nixos" ]]; then
        echo "Creating /etc/nixos directory..."
        sudo mkdir -p /etc/nixos
    fi

    if [[ -L "/etc/nixos/configuration.nix" ]]; then
        echo "Removing stale /etc/nixos/configuration.nix symlink..."
        sudo rm -f "/etc/nixos/configuration.nix"
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

    # Names this run intends to have linked at CONFIG_DIR root — used below
    # to prune anything stale left over from a prior run/migration.
    local -A wanted=()

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
            wanted[$filename]=1
        fi
    done

    if [[ -f "$machine_dir/machine.toml" ]]; then
        target=".system/machines/$machine/machine.toml"
        link="$CONFIG_DIR/machine.toml"
        if [[ ! -L "$link" ]] || [[ "$(readlink "$link")" != "$target" ]]; then
            rm -f "$link"
            ln -s "$target" "$link"
        fi
        wanted[machine.toml]=1
    fi

    # modules/, services/ and local/ are directories, not files — linked
    # separately so the active machine's own modules/services/local can be
    # added/edited straight from CONFIG_DIR root.
    local dirname
    for dirname in modules services local; do
        if [[ -d "$machine_dir/$dirname" ]]; then
            target=".system/machines/$machine/$dirname"
            link="$CONFIG_DIR/$dirname"
            if [[ ! -L "$link" ]] || [[ "$(readlink "$link")" != "$target" ]]; then
                rm -f "$link"
                ln -s "$target" "$link"
            fi
            wanted[$dirname]=1
        fi
    done

    # Prune stale root symlinks: any entry at CONFIG_DIR root that is a
    # symlink (never a real file — those belong to the user) pointing under
    # .system/machines/ but not something this run just (re)linked — e.g.
    # hardware.nix and preferences.nix left dangling after migrating them
    # into local/.
    local entry name entry_target
    for entry in "$CONFIG_DIR"/* "$CONFIG_DIR"/.[!.]*; do
        [[ -L "$entry" ]] || continue
        entry_target=$(readlink "$entry")
        [[ "$entry_target" == .system/machines/* ]] || continue
        name=$(basename "$entry")
        [[ -n "${wanted[$name]:-}" ]] && continue
        rm -f "$entry"
    done
}

#──[Symlink pools]─────────────────────────────────────────────────────────────

LINKS_POOL_ROOT_REL=".system"

# Record that `name` (sourced from `src`) claims a slot in `pool_dir`, erroring
# if a different source already claimed it. Shared by every pool builder below
# so a name must be defined exactly once, regardless of which builder placed
# it or where it physically lands.
_links_claim_name() {
    local -n _seen="$1"
    local name="$2" src="$3" pool_dir="$4"

    if [[ -n "${_seen[$name]:-}" ]]; then
        echo "Error: duplicate '$name' for pool $pool_dir" >&2
        echo "  ${_seen[$name]}" >&2
        echo "  $src" >&2
        echo "  A name must be defined once; rename one of them." >&2
        exit 1
    fi
    _seen[$name]="$src"
}

# Rebuild one pool from scratch: every *.nix found by recursively walking the
# source directories becomes a symlink in the pool, keyed by basename. A pool
# holds nothing but generated symlinks, and every name must be defined exactly
# once across all sources.
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
    local dir src name
    for dir in "$@"; do
        [[ -d "$dir" ]] || continue
        while IFS= read -r -d '' src; do
            name=$(basename "$src")
            _links_claim_name seen "$name" "$src" "$pool_dir"
            ln -s "$src" "$pool_dir/$name"
        done < <(find "$dir" -type f -name '*.nix' -print0)
    done
}

# Rebuild the modules pool. Unlike build_pool, this one is asymmetric by
# origin: machine-authored modules land flat as symlinks, same as any other
# pool. Framework-shipped modules land read-only, as copies rather than
# symlinks, under modules/system/ — editing a system module should fail
# loudly at the OS level, not silently patch the framework repo out from
# under it, and not silently vanish on the next rebuild either. Both sides
# share one name-collision check: a machine cannot claim a name the framework
# already owns, or vice versa — "system" names are reserved.
links::build_modules_pool() {
    local pool_dir="$1" framework_modules_dir="$2"
    shift 2
    local system_dir="$pool_dir/system"

    if [[ -d "$pool_dir" ]]; then
        local existing
        while IFS= read -r -d '' existing; do
            [[ "$existing" == "$system_dir" ]] && continue
            if [[ ! -L "$existing" ]]; then
                echo "Error: real file in generated pool: $existing" >&2
                echo "  Pools contain only symlinks (except modules/system/, which holds" >&2
                echo "  read-only framework copies); move it into a machine's own directory." >&2
                exit 1
            fi
        done < <(find "$pool_dir" -mindepth 1 -maxdepth 1 -print0)
        chmod -R u+w "$system_dir" 2>/dev/null || true  # allow deleting read-only copies
        rm -rf "$pool_dir"
    fi
    mkdir -p "$system_dir"

    local -A seen=()
    local src name

    if [[ -d "$framework_modules_dir" ]]; then
        while IFS= read -r -d '' src; do
            name=$(basename "$src")
            _links_claim_name seen "$name" "$src" "$pool_dir"
            cp "$src" "$system_dir/$name"
            chmod 444 "$system_dir/$name"
        done < <(find "$framework_modules_dir" -type f -name '*.nix' -print0)
    fi

    local dir
    for dir in "$@"; do
        [[ -d "$dir" ]] || continue
        while IFS= read -r -d '' src; do
            name=$(basename "$src")
            _links_claim_name seen "$name" "$src" "$pool_dir"
            ln -s "$src" "$pool_dir/$name"
        done < <(find "$dir" -type f -name '*.nix' -print0)
    done
}

# Rebuild all three pools. Users and services come from the machines that
# author them; modules additionally draw from the framework's own modules/,
# walked recursively so compositor/greeter modules can live in subfolders.
links::build_all_pools() {
    local machines_dir="$CONFIG_DIR/.system/machines"
    local pool_root="$CONFIG_DIR/$LINKS_POOL_ROOT_REL"

    shopt -s nullglob
    links::build_pool "$pool_root/users" "$machines_dir"/*/users
    links::build_pool "$pool_root/services" "$machines_dir"/*/services
    links::build_modules_pool "$pool_root/modules" "$FRAMEWORK_DIR/modules" "$machines_dir"/*/modules
    shopt -u nullglob
}
