#!/usr/bin/env bash
set -euo pipefail

# Shared paths for every internal/ package and bin/ entrypoint.
# The one place both are defined — nothing else may redefine them.

# $HOME becomes /root under sudo, so resolve against the invoking user's
# home (via $SUDO_USER) instead when running as root.
_env_user_home() {
    if [[ -n "${SUDO_USER:-}" ]]; then
        getent passwd "$SUDO_USER" | cut -d: -f6
    else
        echo "$HOME"
    fi
}

FRAMEWORK_DIR="${FRAMEWORK_DIR:-$(_env_user_home)/Workspace/dev/luxos}"
CONFIG_DIR="$(_env_user_home)/.config/luxos"

# Materialized, non-git flake root the active machine's config is built from
# (see internal/staging/staging.sh). Not a git repo on purpose — a flake
# copies only git-tracked files, and this directory must not be subject to
# CONFIG_DIR's gitignore rules.
STAGING_DIR="/etc/nixos/luxos"
