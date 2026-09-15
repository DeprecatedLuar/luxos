#!/usr/bin/env bash
set -euo pipefail

# lux - application entrypoint/router. Sources commands/ and dispatches to them.
# Callers must source internal/env.sh first.

LUX_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LUX_COMMANDS_DIR="$LUX_LIB_DIR/commands"

source "$LUX_LIB_DIR/../flags.sh"
source "$LUX_COMMANDS_DIR/help.sh"
source "$LUX_COMMANDS_DIR/module.sh"
source "$LUX_COMMANDS_DIR/user.sh"

lux::run() {
    local -A opts=()
    local -a rest=()
    # Passthrough: subcommands own their own flags (e.g. `module add
    # --enable`), which this top-level parser doesn't know about. A plain
    # flags::parse would hard-error on those as unrecognized before the
    # subcommand ever sees them.
    flags::parse_passthrough opts rest "help|h:bool" "$@"

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
        module)
            cmd_module "${rest[@]}"
            ;;
        user)
            cmd_user "${rest[@]}"
            ;;
        *)
            echo "Error: unknown command '$cmd'" >&2
            cmd_help >&2
            exit 1
            ;;
    esac
}
