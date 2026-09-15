#!/usr/bin/env bash
set -euo pipefail

# The single owner of the import-line format (implementation-plan.md #18,
# Phase 2). Reads and writes lines inside the one recognized
# "imports = [ ... ];" block of a .local/<host>/modules/default.nix
# entrypoint — hand-written and CLI-written lines alike, indistinguishably.
# Everything else in the file (let, options, comments, a commented-out
# import) is left byte-for-byte (#7). Not a Nix parser: a file with more
# than one recognizable block, or one written some other way (computed
# imports, a single-line block with items), is refused by the writers
# (imports::add/remove) and its lines are skipped, with a warning, by the
# read paths that loop every host (imports::importers/retarget/heal).
#
# Callers must source internal/env.sh (CONFIG_DIR, LOCAL_DIR) first.
# imports::heal also needs configgen::resolve_name, so this file sources
# internal/configgen/configgen.sh itself, matching self-heal.sh's pattern.
#
# Path convention used throughout this file's public API: a "path" is
# relative to CONFIG_DIR/modules with no leading "./" (the same shape
# configgen::resolve_name returns) — imports::list strips it on read,
# imports::add/retarget add it back on write.

IMPORTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$IMPORTS_DIR/../configgen/configgen.sh"

# Recognized import line (implementation-plan.md §3): optional leading
# whitespace, "./" + a path with no whitespace or "#", optional whitespace,
# optional trailing "#..." comment. A line starting with "#" is a plain
# comment and never matches.
IMPORTS_LINE_RE='^[[:space:]]*\./[^[:space:]#]+[[:space:]]*(#.*)?$'

# The block's opening line: "imports = [" alone, or the inline-empty
# "imports = [];" form the scaffold writes. Anything else after the "["
# (e.g. items on the same line) is a shape this package doesn't understand.
IMPORTS_BLOCK_START_RE='^[[:space:]]*imports[[:space:]]*=[[:space:]]*\[[[:space:]]*(\];)?[[:space:]]*$'

# Any line that merely starts an "imports =" assignment — used to detect a
# shape IMPORTS_BLOCK_START_RE doesn't recognize (an "imports =" line exists
# but isn't one of the two forms above).
IMPORTS_LOOSE_START_RE='^[[:space:]]*imports[[:space:]]*=[[:space:]]*\['

# The block's closing line, multi-line form only (the inline-empty form has
# no separate closing line).
IMPORTS_BLOCK_END_RE='^[[:space:]]*];[[:space:]]*$'

#──[Private helpers]────────────────────────────────────────────────────────

# Resolve $1 to the real file behind a possible symlink (the shared mirror
# entrypoint at CONFIG_DIR/modules/default.nix points at a host's real
# .local/<host>/modules/default.nix). Writes always land on this, per §3's
# write rules, so ownership of the hand-owned file survives a run under sudo.
_imports_real_file() {
    readlink -f "$1"
}

# Extract the bare path (no leading "./") from a recognized import line's
# first whitespace-delimited token. The path itself contains no whitespace
# by definition of IMPORTS_LINE_RE, so awk's $1 is exactly it.
_imports_line_path() {
    local token
    token=$(awk '{print $1}' <<< "$1")
    printf '%s\n' "${token#./}"
}

# Everything on the line after the path token and its surrounding
# whitespace — empty, or "#...". Used to preserve a trailing comment when
# rewriting a line.
_imports_line_comment() {
    awk '{ $1=""; sub(/^[[:space:]]+/, ""); print }' <<< "$1"
}

# Name from an import path (implementation-plan.md §3 "Name from an import
# line"): strip a trailing "/default.nix" first (a directory unit named
# explicitly), else strip a trailing ".nix", then take the basename.
#   a/b/hyprland.nix         -> hyprland
#   a/b/hyprland             -> hyprland
#   a/b/hyprland/default.nix -> hyprland
_imports_name_from_path() {
    local path="$1"
    path="${path%/default.nix}"
    path="${path%.nix}"
    basename -- "$path"
}

# Find the single recognizable imports block in $1. Prints
# "start_line end_line inline" (1-indexed, inclusive; inline=1 means the
# block is the single-line "imports = [];" form, start==end, no items) and
# returns 0. Returns 1 with no output when there isn't exactly one, or the
# one present doesn't match either recognized shape — the caller decides
# whether that's a hard error (writers) or a skip (heal/importers/retarget).
_imports_find_block() {
    local file="$1"
    local strict_count loose_count
    strict_count=$(grep -cE "$IMPORTS_BLOCK_START_RE" "$file" || true)
    loose_count=$(grep -cE "$IMPORTS_LOOSE_START_RE" "$file" || true)
    [[ "$strict_count" -eq "$loose_count" ]] || return 1
    [[ "$strict_count" -eq 1 ]] || return 1

    local start_line start_content
    start_line=$(grep -nE "$IMPORTS_BLOCK_START_RE" "$file" | head -1 | cut -d: -f1)
    start_content=$(sed -n "${start_line}p" "$file")

    if grep -qE '\];[[:space:]]*$' <<< "$start_content"; then
        printf '%s %s %s\n' "$start_line" "$start_line" 1
        return 0
    fi

    local end_line
    end_line=$(awk -v s="$start_line" -v re="$IMPORTS_BLOCK_END_RE" \
        'NR>s && $0 ~ re {print NR; exit}' "$file")
    [[ -n "$end_line" ]] || return 1

    printf '%s %s %s\n' "$start_line" "$end_line" 0
}

# Print the standard "shape not recognized" error for $1 and exit 1.
# NOTE: must be called directly from a write path's own function body, never
# from inside a `$(...)` command substitution — exit only terminates the
# subshell command substitution forks, not the calling script.
_imports_shape_error() {
    echo "Error: $1 does not have exactly one recognizable 'imports = [ ... ];' block" >&2
    exit 1
}

# Emit "line_no<TAB>line_text" for every recognized import line inside $1's
# single block. Returns 1 (no output) when the file isn't recognizable —
# the tolerant counterpart to the writers' own _imports_shape_error, for
# callers that loop every host and must not abort on one bad file.
_imports_item_lines() {
    local file="$1"
    local block s e inline
    block=$(_imports_find_block "$file") || return 1
    read -r s e inline <<< "$block"
    [[ "$inline" == "1" ]] && return 0

    local i line
    for ((i = s + 1; i < e; i++)); do
        line=$(sed -n "${i}p" "$file")
        [[ "$line" =~ $IMPORTS_LINE_RE ]] || continue
        printf '%s\t%s\n' "$i" "$line"
    done
}

# Validate $2 (a nameref to a bash array of lines) as the full new content
# of the real file behind $1 with `nix-instantiate --parse`, then replace it
# via temp file + `cat >` (never `mv`), so ownership survives running as
# root (§3's write rules).
_imports_write() {
    local file="$1"
    local -n _imports_write_lines="$2"
    local real
    real=$(_imports_real_file "$file")

    if ! command -v nix-instantiate >/dev/null 2>&1; then
        echo "Error: nix-instantiate not found — required to validate $real before writing" >&2
        exit 1
    fi

    local tmp
    tmp=$(mktemp)
    printf '%s\n' "${_imports_write_lines[@]}" > "$tmp"

    if ! nix-instantiate --parse "$tmp" > /dev/null 2> "$tmp.err"; then
        echo "Error: rewritten $real would fail to parse:" >&2
        cat "$tmp.err" >&2
        rm -f "$tmp" "$tmp.err"
        exit 1
    fi
    rm -f "$tmp.err"

    cat "$tmp" > "$real"
    rm -f "$tmp"
}

#──[Public API]─────────────────────────────────────────────────────────────

# Active import paths in $1, one per line, bare (no leading "./"). Hard
# error if $1 doesn't have exactly one recognizable imports block.
imports::list() {
    local file="$1"
    local rows
    rows=$(_imports_item_lines "$file") || _imports_shape_error "$file"
    [[ -z "$rows" ]] && return 0

    local ln line
    while IFS=$'\t' read -r ln line; do
        _imports_line_path "$line"
    done <<< "$rows"
}

# Append one import line for $2 (bare path, "./" optional) to $1's single
# imports block. Converts the inline-empty "imports = [];" form to
# multi-line as needed. Everything else in the file is left byte-for-byte.
imports::add() {
    local file="$1" path="${2#./}"
    local block s e inline
    block=$(_imports_find_block "$file") || _imports_shape_error "$file"
    read -r s e inline <<< "$block"

    local lines
    mapfile -t lines < "$file"

    local leading
    leading=$(sed -E 's|^([[:space:]]*)imports.*|\1|' <<< "${lines[$((s - 1))]}")

    local new_lines=() idx item_indent
    if [[ "$inline" == "1" ]]; then
        item_indent="${leading}  "
        for idx in "${!lines[@]}"; do
            if [[ $((idx + 1)) -eq "$s" ]]; then
                new_lines+=("${leading}imports = [")
                new_lines+=("${item_indent}./${path}")
                new_lines+=("${leading}];")
            else
                new_lines+=("${lines[$idx]}")
            fi
        done
    else
        item_indent="${leading}  "
        if ((e - s > 1)); then
            local existing
            existing=$(sed -E 's|^([[:space:]]*)\./.*|\1|' <<< "${lines[$s]}")
            [[ -n "$existing" ]] && item_indent="$existing"
        fi
        for idx in "${!lines[@]}"; do
            if [[ $((idx + 1)) -eq "$e" ]]; then
                new_lines+=("${item_indent}./${path}")
            fi
            new_lines+=("${lines[$idx]}")
        done
    fi

    lines=("${new_lines[@]}")
    _imports_write "$file" lines
    echo "added: $file: ./$path"
}

# Delete every import line in $1's single block whose path equals $2 (bare,
# "./" optional). No-op (no write, no print) if none match.
imports::remove() {
    local file="$1" path="${2#./}"
    local block s e inline
    block=$(_imports_find_block "$file") || _imports_shape_error "$file"
    read -r s e inline <<< "$block"
    [[ "$inline" == "1" ]] && return 0

    local lines
    mapfile -t lines < "$file"

    local new_lines=() idx line removed=false
    for idx in "${!lines[@]}"; do
        line="${lines[$idx]}"
        if ((idx + 1 > s && idx + 1 < e)) && [[ "$line" =~ $IMPORTS_LINE_RE ]] \
            && [[ "$(_imports_line_path "$line")" == "$path" ]]; then
            removed=true
            continue
        fi
        new_lines+=("$line")
    done

    $removed || return 0
    lines=("${new_lines[@]}")
    _imports_write "$file" lines
    echo "removed: $file: ./$path"
}

# Every .local/*/modules/default.nix with a line whose name (from its
# path, per _imports_name_from_path) matches $1 — stale paths included.
# Read-only. A host whose file isn't recognizable is silently skipped (same
# tolerance as imports::retarget/heal — it just can't be a hit).
imports::importers() {
    local name="$1"
    local file rows ln line
    for file in "$LOCAL_DIR"/*/"$CONFIGGEN_MODULES_DIR"/default.nix; do
        [[ -f "$file" ]] || continue
        rows=$(_imports_item_lines "$file") || continue
        [[ -z "$rows" ]] && continue

        while IFS=$'\t' read -r ln line; do
            if [[ "$(_imports_name_from_path "$(_imports_line_path "$line")")" == "$name" ]]; then
                echo "$file"
                break
            fi
        done <<< "$rows"
    done
}

# The single cross-host writer (#18): in every .local/*/modules/default.nix,
# rewrite every import line whose name matches $1 to $2 (bare path), or
# delete it when $2 is omitted. Prints each change; a no-op line (already at
# $2) is left untouched and unprinted, so a second run changes nothing.
# Heal, and future rename/remove, go through this — none loop hosts
# themselves. A host whose file isn't recognizable is warned about and left
# untouched, same as heal's own per-file tolerance.
imports::retarget() {
    local name="$1" new_path="${2:-}"
    [[ -n "$new_path" ]] && new_path="${new_path#./}"

    local file rows
    for file in "$LOCAL_DIR"/*/"$CONFIGGEN_MODULES_DIR"/default.nix; do
        [[ -f "$file" ]] || continue
        rows=$(_imports_item_lines "$file") || {
            echo "Warning: $file does not have exactly one recognizable imports block, skipping" >&2
            continue
        }
        [[ -z "$rows" ]] && continue

        local lines
        mapfile -t lines < "$file"

        local -A to_delete=()
        local changed=false
        local ln line old_path
        while IFS=$'\t' read -r ln line; do
            old_path=$(_imports_line_path "$line")
            [[ "$(_imports_name_from_path "$old_path")" == "$name" ]] || continue

            if [[ -n "$new_path" ]]; then
                [[ "$old_path" == "$new_path" ]] && continue
                local leading comment new_line
                leading=$(sed -E 's|^([[:space:]]*).*|\1|' <<< "$line")
                comment=$(_imports_line_comment "$line")
                new_line="${leading}./${new_path}"
                [[ -n "$comment" ]] && new_line="${new_line} ${comment}"
                lines[$((ln - 1))]="$new_line"
                changed=true
                echo "$file: ./$old_path -> ./$new_path"
            else
                to_delete["$ln"]=1
                changed=true
                echo "$file: removed ./$old_path"
            fi
        done <<< "$rows"

        $changed || continue

        if [[ ${#to_delete[@]} -gt 0 ]]; then
            local kept=() idx
            for idx in "${!lines[@]}"; do
                [[ -n "${to_delete[$((idx + 1))]:-}" ]] && continue
                kept+=("${lines[$idx]}")
            done
            lines=("${kept[@]}")
        fi

        _imports_write "$file" lines
    done
}

# Heal every .local/*/modules/default.nix per implementation-plan.md §3:
# a broken line (path doesn't exist under CONFIG_DIR/modules) whose name
# resolves to exactly one unit is retargeted there, across every host that
# references it, via imports::retarget. A broken line whose name resolves
# to nothing is a hard error — or, with $2 (prune) true, removed via
# imports::remove — only in $1 (the active host's real entrypoint file);
# in any other host it's a warning, left untouched (that host errors on its
# own rebuild). Idempotent: a second run finds no broken lines left.
imports::heal() {
    local active_entrypoint="$1" prune="${2:-false}"
    local active_real
    active_real=$(_imports_real_file "$active_entrypoint")

    local file rows
    for file in "$LOCAL_DIR"/*/"$CONFIGGEN_MODULES_DIR"/default.nix; do
        [[ -f "$file" ]] || continue
        rows=$(_imports_item_lines "$file") || {
            echo "Warning: $file does not have exactly one recognizable imports block, skipping heal" >&2
            continue
        }
        [[ -z "$rows" ]] && continue

        local is_active=false
        [[ "$(_imports_real_file "$file")" == "$active_real" ]] && is_active=true

        local ln line path name resolved
        while IFS=$'\t' read -r ln line; do
            path=$(_imports_line_path "$line")
            [[ -e "$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/$path" ]] && continue

            name=$(_imports_name_from_path "$path")
            if resolved=$(configgen::resolve_name "$name"); then
                imports::retarget "$name" "$resolved"
            elif $is_active; then
                if $prune; then
                    imports::remove "$file" "$path"
                    echo "pruned: $file: ./$path ('$name' does not resolve to any module)"
                else
                    echo "Error: $file: ./$path does not exist and '$name' does not resolve to any module" >&2
                    echo "  Fix the import by hand, or rerun with --prune to remove it." >&2
                    exit 1
                fi
            else
                echo "Warning: $file: ./$path does not exist and '$name' does not resolve to any module — left untouched" >&2
            fi
        done <<< "$rows"
    done
}
