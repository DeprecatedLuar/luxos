#!/usr/bin/env bash
# lux flake - manage per-machine flake-sourced package modules
#
# NOTE: not currently wired into main.sh's router — kept here for reference,
# not dispatched.

_flake_sys_dir() {
    local script_dir
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    realpath "$script_dir/../../../.."
}

FLAKE_NAME_RE='^[a-zA-Z0-9_-]+$'

flake_usage() {
    cat <<'EOF'
Usage:
  lux flake add <name>       Open an editor, paste a flake module, add it
  lux flake list             List active and disabled flakes
  lux flake enable <name>    Re-activate a disabled flake
  lux flake disable <name>   Deactivate a flake without deleting it
  lux flake rm <name>        Delete a flake entry permanently
EOF
}

flake_current_machine() {
    local machines_dir host
    machines_dir="$(_flake_sys_dir)/machines"
    host="$(hostname)"
    if [[ ! -d "$machines_dir/$host" ]]; then
        echo "Error: no machine directory found for hostname '$host' in $machines_dir" >&2
        exit 1
    fi
    echo "$host"
}

flake_validate_name() {
    local name=$1
    if [[ ! "$name" =~ $FLAKE_NAME_RE ]]; then
        echo "Error: invalid name '$name' (use letters, numbers, dashes, underscores only)" >&2
        exit 1
    fi
}

flake_pick_editor() {
    if [[ -n "${EDITOR:-}" ]]; then
        echo "$EDITOR"
    elif command -v vi >/dev/null 2>&1; then
        echo "vi"
    elif command -v nano >/dev/null 2>&1; then
        echo "nano"
    else
        echo "Error: no editor found (set \$EDITOR, or install vi/nano)" >&2
        exit 1
    fi
}

flake_add() {
    local name=$1
    flake_validate_name "$name"

    local machine flakes_dir disabled_dir active_file disabled_file
    machine="$(flake_current_machine)"
    flakes_dir="$(_flake_sys_dir)/machines/$machine/flakes"
    disabled_dir="$flakes_dir/disabled"
    active_file="$flakes_dir/$name.nix"
    disabled_file="$disabled_dir/$name.nix"

    if [[ -f "$active_file" || -f "$disabled_file" ]]; then
        echo "Error: a flake named '$name' already exists (use 'lux flake rm $name' first)" >&2
        exit 1
    fi

    local editor tmpfile
    editor="$(flake_pick_editor)"
    tmpfile="$(mktemp --suffix=.nix)"
    trap 'rm -f "$tmpfile"' RETURN

    "$editor" "$tmpfile"

    if [[ ! -s "$tmpfile" ]]; then
        echo "Aborted: empty file, nothing added"
        return
    fi

    if ! nix-instantiate --parse "$tmpfile" >/dev/null 2>"$tmpfile.err"; then
        echo "Error: syntax check failed for '$name':" >&2
        cat "$tmpfile.err" >&2
        rm -f "$tmpfile.err"
        echo "Your content was left at: $tmpfile" >&2
        trap - RETURN
        exit 1
    fi
    rm -f "$tmpfile.err"

    mkdir -p "$flakes_dir"
    mv "$tmpfile" "$active_file"
    trap - RETURN
    echo "Added flake '$name' -> $active_file"
}

flake_list() {
    local machine flakes_dir disabled_dir
    machine="$(flake_current_machine)"
    flakes_dir="$(_flake_sys_dir)/machines/$machine/flakes"
    disabled_dir="$flakes_dir/disabled"

    echo "Machine: $machine"

    echo "Active:"
    if [[ -d "$flakes_dir" ]] && compgen -G "$flakes_dir/*.nix" >/dev/null; then
        for f in "$flakes_dir"/*.nix; do
            echo "  - $(basename "$f" .nix)"
        done
    else
        echo "  (none)"
    fi

    echo "Disabled:"
    if [[ -d "$disabled_dir" ]] && compgen -G "$disabled_dir/*.nix" >/dev/null; then
        for f in "$disabled_dir"/*.nix; do
            echo "  - $(basename "$f" .nix)"
        done
    else
        echo "  (none)"
    fi
}

flake_disable() {
    local name=$1
    flake_validate_name "$name"

    local machine flakes_dir disabled_dir active_file disabled_file
    machine="$(flake_current_machine)"
    flakes_dir="$(_flake_sys_dir)/machines/$machine/flakes"
    disabled_dir="$flakes_dir/disabled"
    active_file="$flakes_dir/$name.nix"
    disabled_file="$disabled_dir/$name.nix"

    if [[ ! -f "$active_file" ]]; then
        echo "Error: no active flake named '$name'" >&2
        exit 1
    fi

    mkdir -p "$disabled_dir"
    mv "$active_file" "$disabled_file"
    echo "Disabled flake '$name'"
}

flake_enable() {
    local name=$1
    flake_validate_name "$name"

    local machine flakes_dir disabled_dir active_file disabled_file
    machine="$(flake_current_machine)"
    flakes_dir="$(_flake_sys_dir)/machines/$machine/flakes"
    disabled_dir="$flakes_dir/disabled"
    active_file="$flakes_dir/$name.nix"
    disabled_file="$disabled_dir/$name.nix"

    if [[ ! -f "$disabled_file" ]]; then
        echo "Error: no disabled flake named '$name'" >&2
        exit 1
    fi

    mkdir -p "$flakes_dir"
    mv "$disabled_file" "$active_file"
    echo "Enabled flake '$name'"
}

flake_rm() {
    local name=$1
    flake_validate_name "$name"

    local machine flakes_dir disabled_dir active_file disabled_file found=0
    machine="$(flake_current_machine)"
    flakes_dir="$(_flake_sys_dir)/machines/$machine/flakes"
    disabled_dir="$flakes_dir/disabled"
    active_file="$flakes_dir/$name.nix"
    disabled_file="$disabled_dir/$name.nix"

    if [[ -f "$active_file" ]]; then
        rm -f "$active_file"
        found=1
    fi
    if [[ -f "$disabled_file" ]]; then
        rm -f "$disabled_file"
        found=1
    fi

    if [[ "$found" -eq 0 ]]; then
        echo "Error: no flake named '$name'" >&2
        exit 1
    fi
    echo "Removed flake '$name'"
}

# cmd_flake - subcommand entrypoint (wire into main.sh's router to use)
cmd_flake() {
    local subcmd="${1:-}"
    case "$subcmd" in
        add)
            [[ $# -eq 2 ]] || { echo "Usage: lux flake add <name>" >&2; exit 1; }
            flake_add "$2"
            ;;
        list)
            flake_list
            ;;
        enable)
            [[ $# -eq 2 ]] || { echo "Usage: lux flake enable <name>" >&2; exit 1; }
            flake_enable "$2"
            ;;
        disable)
            [[ $# -eq 2 ]] || { echo "Usage: lux flake disable <name>" >&2; exit 1; }
            flake_disable "$2"
            ;;
        rm|remove)
            [[ $# -eq 2 ]] || { echo "Usage: lux flake rm <name>" >&2; exit 1; }
            flake_rm "$2"
            ;;
        *)
            flake_usage
            exit 1
            ;;
    esac
}
