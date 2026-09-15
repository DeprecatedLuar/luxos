#!/usr/bin/env bash
set -euo pipefail

# `lux user` — implementation-plan.md #12: the same code as `lux module`
# with the category fixed to "users". Only `add` and `list` actually take a
# category-relative argument, so those two prepend/default to "users" before
# forwarding; edit/enable/disable/remove/rename already operate on bare,
# globally-unique names (#10) and are forwarded untouched — `lux user enable
# foo` and `lux module enable foo` are the same call.
# Callers must source commands/module.sh (for the module:: functions and
# flags::parse) first; bin/lib/lux/main.sh does this.

# `lux user add <name> [--enable]` == `lux module add users/<name> [--enable]`
# (#12) — accepting an already-prefixed "users/<name>" too, so scripts don't
# have to care which form was used to build the argument.
_user_add() {
    local -A opts=()
    local -a rest=()
    flags::parse opts rest "enable:bool" "$@"

    local name="${rest[0]:-}"
    [[ -n "$name" ]] || { echo "Error: usage: lux user add <name> [--enable]" >&2; exit 1; }

    local target="$name"
    [[ "$target" == users/* ]] || target="users/$target"

    local -a fwd=("$target")
    [[ -n "${opts[enable]:-}" ]] && fwd+=(--enable)
    module::add "${fwd[@]}"
}

# `lux user list [subpath]` == `lux module list users[/subpath]`.
_user_list() {
    local sub="${1:-}"
    local category="users"
    [[ -n "$sub" ]] && category="users/$sub"
    module::list "$category"
}

cmd_user() {
    local verb="${1:-}"
    [[ $# -gt 0 ]] && shift

    case "$verb" in
        add)
            _user_add "$@"
            ;;
        list)
            _user_list "${1:-}"
            ;;
        edit)
            module::edit "$@"
            ;;
        enable)
            module::enable "$@"
            ;;
        disable)
            module::disable "$@"
            ;;
        remove|rm)
            module::remove "$@"
            ;;
        rename|rn)
            module::rename "$@"
            ;;
        *)
            echo "Error: unknown user command '$verb'" >&2
            echo "  Usage: lux user <list|add|edit|enable|disable|remove|rename> ..." >&2
            exit 1
            ;;
    esac
}
