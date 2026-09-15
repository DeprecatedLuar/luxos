#!/usr/bin/env bash
set -euo pipefail

# Symlink management: /etc/nixos healing, the framework modules link
# (modules/system -> $FRAMEWORK_DIR/modules), the active host's modules
# mirror under $LOCAL_DIR (its entrypoint symlinks into CONFIG_DIR/modules),
# and the CONFIG_DIR/local -> $LOCAL_DIR/<host> link that makes the active
# host's private tree visible at root alongside modules/.
# Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR, LOCAL_DIR)
# first. Every function here aborts the process (exit 1) on error rather than
# returning non-zero — callers do not check return codes.

LINKS_ETC_NIXOS="/etc/nixos"
LINKS_ETC_NIXOS_ENV_MODE="600"

# Reserved name for the framework's own modules link
# (modules/system -> $FRAMEWORK_DIR/modules). No machine may author it.
LINKS_FRAMEWORK_MODULE_NAME="system"

# Reserved name for CONFIG_DIR/local -> $LOCAL_DIR/<host>. Not a kind; no
# machine may author a real "local" at CONFIG_DIR root.
LINKS_LOCAL_LINK_NAME="local"

#──[/etc/nixos healing]────────────────────────────────────────────────────────

# Ensure /etc/nixos is a real directory holding only hardware-configuration.nix
# and env — configuration.nix now lives under $STAGING_DIR (see
# internal/staging/staging.sh), not here. Idempotent; self-heals a stale
# symlink from the old single-file scheme, and removes the old
# configuration.nix symlink if one is still present from before the flake
# migration.
links::ensure_etc_nixos() {
    if [[ -L "$LINKS_ETC_NIXOS" ]]; then
        echo "Converting $LINKS_ETC_NIXOS from symlink to real directory..."
        sudo rm -f "$LINKS_ETC_NIXOS"
    fi

    if [[ ! -d "$LINKS_ETC_NIXOS" ]]; then
        echo "Creating $LINKS_ETC_NIXOS directory..."
        sudo mkdir -p "$LINKS_ETC_NIXOS"
    fi

    if [[ -L "$LINKS_ETC_NIXOS/configuration.nix" ]]; then
        echo "Removing stale $LINKS_ETC_NIXOS/configuration.nix symlink..."
        sudo rm -f "$LINKS_ETC_NIXOS/configuration.nix"
    fi

    # For service environmentFiles that expect it to exist even when empty
    if [[ ! -f "$LINKS_ETC_NIXOS/env" ]]; then
        echo "Creating $LINKS_ETC_NIXOS/env..."
        sudo touch "$LINKS_ETC_NIXOS/env"
        sudo chmod "$LINKS_ETC_NIXOS_ENV_MODE" "$LINKS_ETC_NIXOS/env"
    fi
}

#──[Framework modules link]─────────────────────────────────────────────────

# Ensure modules/system -> $FRAMEWORK_DIR/modules at CONFIG_DIR root: the
# framework's shipped modules, reached by every host via the same reserved
# name. Errors if a real file/dir already occupies that name (a machine may
# not author "system"), or if the framework modules directory itself is
# missing. Idempotent.
links::ensure_framework_link() {
    local target_dir="$CONFIG_DIR/modules"
    local system_link="$target_dir/$LINKS_FRAMEWORK_MODULE_NAME"

    if [[ ! -d "$FRAMEWORK_DIR/modules" ]]; then
        echo "Error: framework modules directory not found: $FRAMEWORK_DIR/modules" >&2
        exit 1
    fi

    mkdir -p "$target_dir"

    if [[ -e "$system_link" && ! -L "$system_link" ]]; then
        echo "Error: '$system_link' is a real file/dir." >&2
        echo "  '$LINKS_FRAMEWORK_MODULE_NAME' is reserved for the framework modules link; nothing else may claim it." >&2
        exit 1
    fi

    ln -sfn "$FRAMEWORK_DIR/modules" "$system_link"
}

#──[Per-host mirror]────────────────────────────────────────────────────────

# Ensure $LOCAL_DIR/<host>/modules/ exists and its entrypoint (hand-written)
# is symlinked into CONFIG_DIR/modules/default.nix. Errors if a real file
# already occupies that path — CONFIG_DIR/modules/default.nix is always the
# generated mirror link, never hand-authored. Idempotent; rebuilt on every
# self-heal.
links::ensure_mirror() {
    local host="$1"
    local host_modules_dir="$LOCAL_DIR/$host/$CONFIGGEN_MODULES_DIR"
    local shared_default="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/default.nix"

    mkdir -p "$host_modules_dir"

    if [[ -e "$shared_default" && ! -L "$shared_default" ]]; then
        echo "Error: '$shared_default' is a real file." >&2
        echo "  It's reserved for the generated mirror link to $host_modules_dir/default.nix." >&2
        exit 1
    fi

    ln -sfn "../.local/$host/$CONFIGGEN_MODULES_DIR/default.nix" "$shared_default"
}

# Ensure CONFIG_DIR/local -> $LOCAL_DIR/<host>: the active host's whole
# private tree, visible at root alongside the shared kind folders. Errors if a
# real file/dir already occupies that name. Idempotent; rebuilt on every
# self-heal, so it always points at whichever host is currently active.
links::ensure_local_link() {
    local host="$1"
    local link="$CONFIG_DIR/$LINKS_LOCAL_LINK_NAME"

    if [[ -e "$link" && ! -L "$link" ]]; then
        echo "Error: '$link' is a real file/dir." >&2
        echo "  '$LINKS_LOCAL_LINK_NAME' is reserved for the link to the active host's $LOCAL_DIR/$host; nothing else may claim it." >&2
        exit 1
    fi

    ln -sfn "$LOCAL_DIR/$host" "$link"
}
