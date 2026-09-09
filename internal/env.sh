#!/usr/bin/env bash
set -euo pipefail

# Shared paths for every internal/ package and bin/ entrypoint.
# The one place both are defined — nothing else may redefine them.

# Installed location of the framework once packaged for a target machine.
LUXOS_INSTALL_DIR="/etc/luxos"

# $HOME becomes /root under sudo, so resolve against the invoking user's
# home (via $SUDO_USER) instead when running as root.
_env_user_home() {
    if [[ -n "${SUDO_USER:-}" ]]; then
        getent passwd "$SUDO_USER" | cut -d: -f6
    else
        echo "$HOME"
    fi
}

# Resolution order: explicit FRAMEWORK_DIR in the environment wins ->
# LUXOS_INSTALL_DIR if it exists -> the dev checkout if it exists -> hard
# error naming both paths tried. No silent fallback.
_env_resolve_framework_dir() {
    if [[ -n "${FRAMEWORK_DIR:-}" ]]; then
        echo "$FRAMEWORK_DIR"
        return
    fi

    if [[ -d "$LUXOS_INSTALL_DIR" ]]; then
        echo "$LUXOS_INSTALL_DIR"
        return
    fi

    local dev_dir="$(_env_user_home)/Workspace/dev/luxos"
    if [[ -d "$dev_dir" ]]; then
        echo "$dev_dir"
        return
    fi

    echo "luxos: cannot locate framework directory; tried '$LUXOS_INSTALL_DIR' and '$dev_dir'" >&2
    exit 1
}

FRAMEWORK_DIR="$(_env_resolve_framework_dir)"
CONFIG_DIR="$(_env_user_home)/.config/luxos"

# Materialized, non-git flake root the active machine's config is built from
# (see internal/staging/staging.sh). Not a git repo on purpose — a flake
# copies only git-tracked files, and this directory must not be subject to
# CONFIG_DIR's gitignore rules.
STAGING_DIR="/etc/nixos/luxos"
