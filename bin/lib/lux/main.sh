#!/usr/bin/env bash
set -euo pipefail

# lux - application entrypoint/router. Sources commands/ and dispatches to them.
# Callers must source internal/env.sh first.

LUX_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LUX_COMMANDS_DIR="$LUX_LIB_DIR/commands"

source "$LUX_LIB_DIR/../flags.sh"
source "$LUX_COMMANDS_DIR/help.sh"

lux::run() {
    local -A opts=()
    local -a rest=()
    flags::parse opts rest "help|h:bool" "$@"

    if [[ -n "${opts[help]:-}" ]]; then
        cmd_help
        return
    fi

    local cmd="${rest[0]:-help}"
    [[ ${#rest[@]} -gt 0 ]] && rest=("${rest[@]:1}")

    case "$cmd" in
        help|h)
            cmd_help
            ;;
        *)
            echo "Error: unknown command '$cmd'" >&2
            cmd_help >&2
            exit 1
            ;;
    esac
}
