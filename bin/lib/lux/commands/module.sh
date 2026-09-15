#!/usr/bin/env bash
set -euo pipefail

# `lux module` — Phase 5 of implementation-plan.md. list/add/edit/enable/
# disable/remove/rename over CONFIG_DIR/modules, on top of configgen's walk/
# resolve and the imports package's single-owner access to the active host's
# entrypoint (implementation-plan.md #18). `lux user` (commands/user.sh) is
# the same code with the category fixed to "users" (#12) — everything here
# operates on bare, globally-unique names (#10) except `add`/`list`, which
# take a category-relative path.
# Callers must source internal/env.sh and bin/lib/flags.sh first; this file
# sources internal/imports/imports.sh (which sources configgen.sh) itself,
# matching bin/lib/nixos-rebuild/main.sh's pattern.

source "$FRAMEWORK_DIR/internal/imports/imports.sh"
source "$FRAMEWORK_DIR/internal/refs/refs.sh"

MODULE_CMD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Truth source for state markers (implementation-plan.md #23) — the running
# generation's copy of the staged entrypoint (system.nix wires this up via
# environment.etc."luxos/modules.nix"). Not $STAGING_DIR, which dry-build and
# failed builds rewrite. Kept as one named constant so the public entrypoints
# below reference it exactly once; _module_render_list itself takes the
# running-file path as a parameter so it can be exercised against a fake file
# in tests without touching /run.
MODULE_RUNNING_FILE="/run/current-system/etc/luxos/modules.nix"

# Minimal scaffold for a non-user module (implementation-plan.md Phase 5
# "add" — "a simple template"). Single-file, per #19's "single-file modules
# stay single files until they grow".
MODULE_SIMPLE_TEMPLATE=$'{ ... }:\n\n{\n}\n'

# User template lives at bin/lib/setup/templates/user — reused verbatim
# (implementation-plan.md #19), not reinvented here. MODULE_USER_PLACEHOLDER
# duplicates setup/main.sh's SETUP_USER_PLACEHOLDER literal on purpose: same
# convention as CONFIGGEN_LOCAL_LINK_NAME ("its own copy of the same literal
# rather than a cross-file reference") since the two commands don't share a
# common internal/ package.
MODULE_USER_TEMPLATE_DIR="$MODULE_CMD_DIR/../../setup/templates/user"
MODULE_USER_PLACEHOLDER="users.users.user ="

# Reserved category name (implementation-plan.md, links::ensure_framework_link):
# modules/system is the framework's own link. Nothing under it is owned by
# this CONFIG_DIR, so add/remove/rename refuse to touch it.
MODULE_FRAMEWORK_CATEGORY="system"

#──[State markers]───────────────────────────────────────────────────────────

# Pure: $1/$2 are "true"/"false" (enabled?, running?). Prints the one marker
# character for that combination (Phase 5's state-marker table).
_module_marker() {
    local enabled="$1" running="$2"
    if [[ "$enabled" == true && "$running" == true ]]; then
        echo "◉"
    elif [[ "$enabled" == true && "$running" == false ]]; then
        echo "⊕"
    elif [[ "$enabled" == false && "$running" == true ]]; then
        echo "⊘"
    else
        echo "○"
    fi
}

# Pure: sort rank for a marker, per Phase 5's "ordered by that state order"
# (⊕, ◉, ⊘, ○), lower sorts first.
_module_marker_rank() {
    case "$1" in
        "⊕") echo 0 ;;
        "◉") echo 1 ;;
        "⊘") echo 2 ;;
        "○") echo 3 ;;
    esac
}

# Populate nameref $2 (an associative array) with name -> 1 for every active
# import line's name in file $1. Missing file (no running generation yet, or
# no entrypoint healed yet) is treated as "nothing enabled/running" rather
# than an error — list's own caller reports that case explicitly.
_module_name_set() {
    local file="$1"
    local -n _module_name_set_out="$2"
    _module_name_set_out=()
    [[ -f "$file" ]] || return 0

    local path name
    while IFS= read -r path; do
        [[ -n "$path" ]] || continue
        name=$(imports::name_from_path "$path")
        _module_name_set_out["$name"]=1
    done < <(imports::list "$file")
}

# The bare import path (as actually written, possibly stale) for $2's name
# in $1, or nothing + failure if $2 isn't enabled there.
_module_enabled_path_for_name() {
    local file="$1" name="$2"
    [[ -f "$file" ]] || return 1

    local path
    while IFS= read -r path; do
        [[ -n "$path" ]] || continue
        [[ "$(imports::name_from_path "$path")" == "$name" ]] && { echo "$path"; return 0; }
    done < <(imports::list "$file")
    return 1
}

#──[list]─────────────────────────────────────────────────────────────────

# Sort "category<TAB>name<TAB>marker<TAB>rank" rows by state order then name
# (Phase 5), LC_ALL=C.
_module_sort_rows() {
    LC_ALL=C sort -t $'\t' -k4,4n -k2,2
}

# TTY rendering: root units (category "") first with no header, then every
# other category present, full-path headers, LC_ALL=C sorted, each group
# ordered by _module_sort_rows.
_module_render_tty() {
    local rows="$1"

    if [[ -z "$rows" ]]; then
        echo "(no modules)"
        return 0
    fi

    local root_rows
    root_rows=$(awk -F'\t' '$1=="."' <<< "$rows" | _module_sort_rows)
    if [[ -n "$root_rows" ]]; then
        while IFS=$'\t' read -r _ name marker _; do
            printf '%s %s\n' "$marker" "$name"
        done <<< "$root_rows"
    fi

    local cats
    cats=$(awk -F'\t' '$1!="."{print $1}' <<< "$rows" | LC_ALL=C sort -u)
    [[ -z "$cats" ]] && return 0

    local cat group
    while IFS= read -r cat; do
        [[ -n "$cat" ]] || continue
        echo "$cat:"
        group=$(awk -F'\t' -v c="$cat" '$1==c' <<< "$rows" | _module_sort_rows)
        while IFS=$'\t' read -r _ name marker _; do
            printf '  %s %s\n' "$marker" "$name"
        done <<< "$group"
    done <<< "$cats"
}

# Piped rendering: one "category/name" per line (bare "name" for root
# units), no headers, LC_ALL=C sorted — so grep/fzf keep category context.
_module_render_plain() {
    local rows="$1"
    [[ -z "$rows" ]] && return 0
    awk -F'\t' '{ if ($1==".") print $2; else print $1"/"$2 }' <<< "$rows" | LC_ALL=C sort
}

# The testable core of `list`: given the active entrypoint and the running
# file explicitly (rather than the MODULE_RUNNING_FILE constant), builds the
# rows and renders them per $3 (tty|plain). Kept separate from module::list
# so a test can exercise the enabled/running comparison against a fake
# "running" file without touching /run/current-system.
_module_render_list() {
    local entrypoint="$1" running_file="$2" mode="$3" category_path="$4"

    local all
    all=$(configgen::walk_units)

    local filtered="$all"
    if [[ -n "$category_path" ]]; then
        filtered=$(awk -F'\t' -v p="$category_path/" 'index($2, p) == 1' <<< "$all")
    fi

    local -A enabled=() running=()
    _module_name_set "$entrypoint" enabled
    _module_name_set "$running_file" running

    # Category is dirname(path), keeping bash's own "." for root units as
    # the row's sentinel rather than blanking it out — `read`'s IFS=$'\t'
    # strips a genuinely empty *leading* field (tab is IFS whitespace, so
    # this happens even with IFS restricted to just it), which would corrupt
    # every root-unit row read back out of these rows below.
    local rows="" name path cat en rn marker rank
    while IFS=$'\t' read -r name path; do
        [[ -n "$name" ]] || continue
        cat=$(dirname "$path")
        en=false; rn=false
        [[ -n "${enabled[$name]:-}" ]] && en=true
        [[ -n "${running[$name]:-}" ]] && rn=true
        marker=$(_module_marker "$en" "$rn")
        rank=$(_module_marker_rank "$marker")
        rows+="$cat"$'\t'"$name"$'\t'"$marker"$'\t'"$rank"$'\n'
    done <<< "$filtered"
    rows="${rows%$'\n'}"

    if [[ "$mode" == "tty" ]]; then
        _module_render_tty "$rows"
    else
        _module_render_plain "$rows"
    fi
}

# `lux module list [category-path]`. Unknown category is a hard error, not
# an empty list. TTY vs. piped is [[ -t 1 ]] — see _module_render_tty/plain.
module::list() {
    local category_path="${1:-}"
    category_path="${category_path%/}"

    local modules_root="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR"
    if [[ -n "$category_path" ]]; then
        local cat_dir="$modules_root/$category_path"
        if [[ ! -d "$cat_dir" ]]; then
            echo "Error: unknown category '$category_path'" >&2
            exit 1
        fi
        if [[ -f "$cat_dir/default.nix" ]]; then
            echo "Error: '$category_path' is a module, not a category" >&2
            exit 1
        fi
    fi

    local entrypoint="$modules_root/default.nix"
    local mode="plain"
    [[ -t 1 ]] && mode="tty"

    if [[ ! -f "$MODULE_RUNNING_FILE" ]]; then
        echo "Note: no running generation found at $MODULE_RUNNING_FILE — state unknown until the next switch; every enabled module shows as staged." >&2
    fi

    _module_render_list "$entrypoint" "$MODULE_RUNNING_FILE" "$mode" "$category_path"
}

#──[add]──────────────────────────────────────────────────────────────────

# Scaffold modules/<target-category>/<name> from the user template
# (implementation-plan.md #19), filling in the literal account name.
_module_add_user() {
    local target="$1"
    local name
    name=$(basename "$target")
    local dest="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/$target"

    mkdir -p "$dest"
    cp "$MODULE_USER_TEMPLATE_DIR/default.nix" "$dest/default.nix"

    local template
    template="$(< "$MODULE_USER_TEMPLATE_DIR/account.nix")"
    if [[ "$template" != *"$MODULE_USER_PLACEHOLDER"* ]]; then
        echo "Error: $MODULE_USER_TEMPLATE_DIR/account.nix has no '$MODULE_USER_PLACEHOLDER' to fill in" >&2
        exit 1
    fi
    printf '%s\n' "${template//"$MODULE_USER_PLACEHOLDER"/users.users.$name =}" > "$dest/account.nix"
    echo "Created modules/$target/{default.nix,account.nix}"
}

# `lux module add <category/name> [--enable]`. Never enables unless
# --enable (#22). Picks the user template when the target category is
# (under) users/, regardless of whether it was reached via `lux module add
# users/<name>` or `lux user add <name>` (#12).
module::add() {
    local -A opts=()
    local -a rest=()
    flags::parse opts rest "enable:bool" "$@"

    local target="${rest[0]:-}"
    if [[ -z "$target" || "$target" == */ || "$target" == /* ]]; then
        echo "Error: usage: lux module add <category/name> [--enable]" >&2
        exit 1
    fi

    local name
    name=$(basename "$target")

    local existing
    if existing=$(configgen::resolve_name "$name" 2>/dev/null); then
        echo "Error: module name '$name' already exists at modules/$existing" >&2
        exit 1
    fi

    local modules_root="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR"
    if [[ -e "$modules_root/$target" || -e "$modules_root/$target.nix" ]]; then
        echo "Error: 'modules/$target' already exists" >&2
        exit 1
    fi

    mkdir -p "$modules_root/$(dirname "$target")"

    if [[ "$target" == users/* || "$target" == "users" ]]; then
        _module_add_user "$target"
    else
        printf '%s' "$MODULE_SIMPLE_TEMPLATE" > "$modules_root/$target.nix"
        echo "Created modules/$target.nix"
    fi

    if [[ -n "${opts[enable]:-}" ]]; then
        module::enable "$name"
    fi
}

#──[edit]─────────────────────────────────────────────────────────────────

# `lux module edit <name>` — $EDITOR on the file unit, or the folder unit's
# default.nix.
module::edit() {
    local name="${1:-}"
    [[ -n "$name" ]] || { echo "Error: usage: lux module edit <name>" >&2; exit 1; }

    local path
    path=$(configgen::resolve_name "$name") || { echo "Error: unknown module '$name'" >&2; exit 1; }

    local target="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/$path"
    local file="$target"
    [[ -d "$target" ]] && file="$target/default.nix"

    [[ -n "${EDITOR:-}" ]] || { echo "Error: \$EDITOR is not set" >&2; exit 1; }

    "$EDITOR" "$file"
}

#──[enable/disable]─────────────────────────────────────────────────────────

# Shared body for enable/disable: $1 name, $2 "enable"|"disable". Active
# host's entrypoint via imports::add|remove (#18) — never touches other
# hosts. Unrecognized file shapes are refused by imports::add/remove
# themselves. Already-in-desired-state is reported and exits 0.
_module_toggle() {
    local name="$1" action="$2"
    [[ -n "$name" ]] || { echo "Error: usage: lux module $action <name>" >&2; exit 1; }

    local path
    path=$(configgen::resolve_name "$name") || { echo "Error: unknown module '$name'" >&2; exit 1; }

    local entrypoint="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/default.nix"
    [[ -f "$entrypoint" ]] || {
        echo "Error: no active host entrypoint at $entrypoint — run setup or nixos-rebuild first" >&2
        exit 1
    }

    local existing_path
    if [[ "$action" == "enable" ]]; then
        if existing_path=$(_module_enabled_path_for_name "$entrypoint" "$name"); then
            echo "'$name' is already enabled"
            return 0
        fi
        imports::add "$entrypoint" "$path"
    else
        if ! existing_path=$(_module_enabled_path_for_name "$entrypoint" "$name"); then
            echo "'$name' is already disabled"
            return 0
        fi
        imports::remove "$entrypoint" "$existing_path"
    fi
}

module::enable() { _module_toggle "${1:-}" enable; }
module::disable() { _module_toggle "${1:-}" disable; }

#──[remove]───────────────────────────────────────────────────────────────

# `lux module remove|rm <name> [-y]` (#22): lists every entrypoint from
# imports::importers, y/N (skipped with -y), then imports::retarget removes
# it from every host and the unit is deleted.
module::remove() {
    local -A opts=()
    local -a rest=()
    # "y|y" (not just "y") because flags.sh's short-flag map is only
    # populated from an explicit alias — a bare single-char name registers
    # --y, not -y.
    flags::parse opts rest "y|y:bool" "$@"

    local name="${rest[0]:-}"
    [[ -n "$name" ]] || { echo "Error: usage: lux module remove <name> [-y]" >&2; exit 1; }

    local path
    path=$(configgen::resolve_name "$name") || { echo "Error: unknown module '$name'" >&2; exit 1; }

    if [[ "$path" == "$MODULE_FRAMEWORK_CATEGORY"/* ]]; then
        echo "Error: '$name' is a framework module (modules/$path) — not owned by this config" >&2
        exit 1
    fi

    local importers
    importers=$(imports::importers "$name")
    if [[ -n "$importers" ]]; then
        echo "'$name' is imported by:"
        while IFS= read -r f; do
            [[ -n "$f" ]] && echo "  $f"
        done <<< "$importers"
    else
        echo "'$name' is not imported by any host."
    fi

    local dependents
    dependents=$(refs::dependents "$name")
    if [[ -n "$dependents" ]]; then
        echo "'$name' is referenced by:"
        while IFS= read -r f; do
            [[ -n "$f" ]] && echo "  $f"
        done <<< "$dependents"
    fi

    if [[ -z "${opts[y]:-}" ]]; then
        local reply
        read -rp "Remove modules/$path and the import line(s)/reference(s) above? [y/N] " reply
        [[ "$reply" =~ ^[Yy]$ ]] || { echo "Aborted."; return 0; }
    fi

    # refs::retarget first: it validates every dependent's call shape
    # (refs::names) before writing anything and refuses the whole operation
    # on a bad shape (#30 item 20) — doing it before imports::retarget/rm
    # means a refusal here leaves the tree completely untouched.
    refs::retarget "$name"
    imports::retarget "$name"
    rm -rf -- "$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/$path"
    echo "Removed modules/$path"
}

#──[rename]───────────────────────────────────────────────────────────────

# `lux module rename|rn <old> <new> [-y]`: moves the unit (same category,
# new basename — renaming is identity, not relocation, #21) then
# imports::retarget <old> <new-path> across every host. When any module file
# references <old> via luxos.modules (refs::dependents), prompts listing
# them first (-y skips, same convention as `remove`'s skip flag) — no
# dependents means no prompt, behavior unchanged from before this existed.
module::rename() {
    local -A opts=()
    local -a rest=()
    flags::parse opts rest "y|y:bool" "$@"

    local old="${rest[0]:-}" new="${rest[1]:-}"
    if [[ -z "$old" || -z "$new" ]]; then
        echo "Error: usage: lux module rename <old> <new> [-y]" >&2
        exit 1
    fi

    local old_path
    old_path=$(configgen::resolve_name "$old") || { echo "Error: unknown module '$old'" >&2; exit 1; }

    if [[ "$old_path" == "$MODULE_FRAMEWORK_CATEGORY"/* ]]; then
        echo "Error: '$old' is a framework module (modules/$old_path) — not owned by this config" >&2
        exit 1
    fi

    if configgen::resolve_name "$new" >/dev/null 2>&1; then
        local existing
        existing=$(configgen::resolve_name "$new")
        echo "Error: module name '$new' already exists at modules/$existing" >&2
        exit 1
    fi

    local dependents
    dependents=$(refs::dependents "$old")
    if [[ -n "$dependents" ]]; then
        echo "'$old' is referenced by:"
        while IFS= read -r f; do
            [[ -n "$f" ]] && echo "  $f"
        done <<< "$dependents"

        if [[ -z "${opts[y]:-}" ]]; then
            local reply
            read -rp "Rename '$old' to '$new' and update the reference(s) above? [y/N] " reply
            [[ "$reply" =~ ^[Yy]$ ]] || { echo "Aborted."; return 0; }
        fi
    fi

    local modules_root="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR"
    local old_full="$modules_root/$old_path"
    local category
    category=$(dirname "$old_path")
    [[ "$category" == "." ]] && category=""

    local new_path
    if [[ -d "$old_full" ]]; then
        new_path="${category:+$category/}$new"
    else
        new_path="${category:+$category/}$new.nix"
    fi

    # refs::retarget first (validates every dependent's call shape before
    # writing anything, and refuses the whole operation on a bad shape — #30
    # item 20): a refusal here leaves the unit unmoved and every host
    # entrypoint untouched, rather than moving the file first and finding
    # out afterward that a dependent couldn't be rewritten.
    refs::retarget "$old" "$new"
    mv -- "$old_full" "$modules_root/$new_path"
    imports::retarget "$old" "$new_path"

    if [[ "$category" == "users" || "$category" == users/* ]]; then
        echo "Note: the account name in modules/$new_path/account.nix is still '$old' — rename it there by hand if the actual system user should change too."
    fi

    echo "Renamed modules/$old_path -> modules/$new_path"
}

#──[Dispatcher]───────────────────────────────────────────────────────────

cmd_module() {
    local verb="${1:-}"
    [[ $# -gt 0 ]] && shift

    case "$verb" in
        list)
            module::list "${1:-}"
            ;;
        add)
            module::add "$@"
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
            echo "Error: unknown module command '$verb'" >&2
            echo "  Usage: lux module <list|add|edit|enable|disable|remove|rename> ..." >&2
            exit 1
            ;;
    esac
}
