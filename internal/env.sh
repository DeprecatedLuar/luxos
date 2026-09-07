#!/usr/bin/env bash
set -euo pipefail

# Shared paths for every internal/ package and bin/ entrypoint.
# The one place both are defined — nothing else may redefine them.

FRAMEWORK_DIR="$HOME/Workspace/dev/luxos"
CONFIG_DIR="$HOME/.config/luxos"
