#!/usr/bin/env bash
set -euo pipefail

# Materializes the active host's flake root at $STAGING_DIR: a real-file copy
# of CONFIG_DIR/modules (symlinks dereferenced — modules/system and the
# active host's entrypoint link alike) plus that host's own $LOCAL_DIR/<host>
# tree, and a whitelisted slice of the framework, since a flake evaluates a
# copy of its own directory and can't reach outside it. The two roots are
# copied separately rather than `cp -rL $CONFIG_DIR` wholesale, which would
# also sweep up channels.toml, .git, and every other host's $LOCAL_DIR entry.
# flake.nix and configuration.nix are NOT copied here: configgen writes them
# straight into $STAGING_DIR after materialize runs, since their imports
# only resolve at that location (see internal/self-heal/self-heal.sh).
# Callers must source internal/env.sh first.
# Every function here aborts the process (exit 1) on error rather than
# returning non-zero — callers do not check return codes.

# Marks a directory as a staging tree this package created and may wipe.
STAGING_MARKER=".luxos-staging"

# The only paths Nix reads out of the framework tree, relative to FRAMEWORK_DIR.
# Anything not listed never reaches the staging flake root. "modules" isn't
# here: framework modules arrive through modules/system
# (-> $FRAMEWORK_DIR/modules, dereferenced by the cp -rL below), not through
# a separate framework-side copy.
STAGING_FRAMEWORK_PATHS=(
    system.nix            # imported by every generated configuration.nix
    internal/env.sh       # readFile'd into the nixos-rebuild bootstrap
)

# The one file staging pulls from /etc/nixos rather than from either repo.
STAGING_HARDWARE_CONFIG="/etc/nixos/hardware-configuration.nix"

# Every staged file's mode, matching hardware-configuration.nix and
# flake.lock. install_generated needs this explicitly: cp preserves a new
# destination's mode from its source, and mktemp's temp files default to
# 600 — without this they'd be the one root-only-readable file in an
# otherwise 644 tree, unreadable by a non-root `nix flake show`/`nix eval`.
STAGING_GENERATED_MODE="644"

# Refuse to touch $STAGING_DIR unless it's empty or one we generated
# ourselves.
_staging_guard() {
    if [[ -e "$STAGING_DIR" && ! -f "$STAGING_DIR/$STAGING_MARKER" ]]; then
        echo "Error: $STAGING_DIR exists but isn't a luxos-managed staging tree" >&2
        echo "  Remove it manually first if that's intended." >&2
        exit 1
    fi
}

# cp -rL fails hard on a dangling symlink and its error doesn't name the
# culprit usefully, so catch it here first and report exactly which link
# is broken. No error swallowing: a find failure (e.g. permission denied
# partway through the tree) is a real error, not "no dangling links".
# Takes one or more directories — the shared kind folders plus the active
# host's own $LOCAL_DIR tree.
_staging_check_no_dangling_links() {
    local dangling
    dangling="$(find "$@" -xtype l)"
    if [[ -n "$dangling" ]]; then
        echo "Error: dangling symlink(s) — cannot stage:" >&2
        echo "$dangling" | while IFS= read -r line; do echo "  $line" >&2; done
        exit 1
    fi
}

# Copies the whitelisted framework paths into $dest, preserving each path's
# position relative to FRAMEWORK_DIR so relative imports inside them (e.g.
# system.nix's ../hardware-configuration.nix) still resolve.
_staging_copy_framework() {
    local dest="$1"
    local rel_path src_path dest_path
    for rel_path in "${STAGING_FRAMEWORK_PATHS[@]}"; do
        src_path="$FRAMEWORK_DIR/$rel_path"
        dest_path="$dest/$rel_path"
        if [[ ! -e "$src_path" ]]; then
            echo "Error: $src_path (from STAGING_FRAMEWORK_PATHS) does not exist" >&2
            exit 1
        fi
        sudo mkdir -p "$(dirname "$dest_path")"
        sudo cp -r "$src_path" "$dest_path"
    done
}

staging::materialize() {
    local host="$1"
    local host_dir="$LOCAL_DIR/$host"
    local modules_dir="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR"

    _staging_guard
    _staging_check_no_dangling_links "$host_dir" "$modules_dir"

    sudo rm -rf "$STAGING_DIR"
    sudo mkdir -p "$STAGING_DIR/framework" "$STAGING_DIR/config"
    sudo touch "$STAGING_DIR/$STAGING_MARKER"

    _staging_copy_framework "$STAGING_DIR/framework"

    # Dereferenced — this turns both modules/system (the framework link) and
    # the active host's entrypoint symlink into real files. Then the active
    # host's own private tree.
    sudo cp -rL "$modules_dir" "$STAGING_DIR/config/$CONFIGGEN_MODULES_DIR"
    sudo cp -rL "$host_dir" "$STAGING_DIR/config/local"

    sudo cp "$STAGING_HARDWARE_CONFIG" "$STAGING_DIR/hardware-configuration.nix"

    if [[ -f "$CONFIG_DIR/flake.lock" ]]; then
        sudo cp "$CONFIG_DIR/flake.lock" "$STAGING_DIR/flake.lock"
    fi
}

# Install a locally-written temp file as $STAGING_DIR/$rel_dest. Generators
# (configgen::generate, configgen::generate_flake) run unprivileged and
# write to a temp file first; this is the one place that escalates to write
# into $STAGING_DIR, so the sudo call lives here rather than in configgen.
staging::install_generated() {
    local src="$1" rel_dest="$2"
    local dest="$STAGING_DIR/$rel_dest"
    sudo mkdir -p "$(dirname "$dest")"
    sudo cp "$src" "$dest"
    sudo chmod "$STAGING_GENERATED_MODE" "$dest"
}

# Re-lock the staged flake and copy the result back to CONFIG_DIR to be
# committed. The only writer of CONFIG_DIR/flake.lock outside a human editor.
staging::update_lock() {
    sudo nix flake update --flake "$STAGING_DIR"
    sudo install -m 644 -o "$(id -u)" -g "$(id -g)" \
        "$STAGING_DIR/flake.lock" "$CONFIG_DIR/flake.lock"
    echo "Updated $CONFIG_DIR/flake.lock — commit it."
}
