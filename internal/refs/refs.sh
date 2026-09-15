#!/usr/bin/env bash
set -euo pipefail

# The single owner of what a module file under CONFIG_DIR/modules may
# reference (implementation-plan.md #25, #26, #27, #28, §3 "Module boundary"
# and "luxos.modules") — paths, and luxos.modules names — as internal/imports/
# imports.sh is for host entrypoints (#18). Everything goes through
# `nix-instantiate --parse`; no hand-written Nix parsing.
#
# Callers must source internal/env.sh (CONFIG_DIR) first. This file also
# sources internal/configgen/configgen.sh itself (CONFIGGEN_MODULES_DIR,
# configgen::walk_units, configgen::resolve_name), matching imports.sh's own
# pattern.
#
# Path convention: every public function here takes an ABSOLUTE file path.
# nix-instantiate --parse resolves relative paths inside a file lexically
# against the path it was given, but a relative *argument* resolves against
# the physical cwd — walking through a symlinked CONFIG_DIR (the common case:
# ~/.config/luxos) would then silently resolve against the link target
# instead of the link. Passing an absolute path sidesteps that; callers are
# responsible for it (refs::validate itself always does, since it walks from
# $CONFIG_DIR/$CONFIGGEN_MODULES_DIR, which is already absolute).

REFS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$REFS_DIR/../configgen/configgen.sh"

#──[Private: parsing helpers]───────────────────────────────────────────────

# Reject a non-absolute file argument up front — see the module-level comment
# above for why. Prints nothing; caller checks the exit status.
_refs_require_absolute() {
    [[ "$1" == /* ]]
}

# Strip every double-quoted string literal from nix-instantiate --parse
# output (which normalizes all strings, ''indented'' included, to "..." with
# backslash escapes), so a path-looking or name-looking substring inside a
# string can never match downstream regexes.
_refs_strip_strings() {
    awk '
    {
        out = ""; n = length($0)
        for (i = 1; i <= n; i++) {
            c = substr($0, i, 1)
            if (instr) {
                if (esc) { esc = 0 }
                else if (c == "\\") { esc = 1 }
                else if (c == "\"") { instr = 0 }
            } else if (c == "\"") { instr = 1 }
            else { out = out c }
        }
        print out
    }'
}

# Parse $1 (must be absolute). Prints the parse output and returns 0, or
# prints nothing and returns 1 with the parser's error on stderr. Never
# aborts the process itself — callers decide (refs::validate collects
# failures across the whole tree instead of stopping at the first).
_refs_parse() {
    local file="$1" out
    if ! _refs_require_absolute "$file"; then
        echo "refs: $file is not an absolute path (nix-instantiate resolves relative paths against the physical cwd)" >&2
        return 1
    fi
    out=$(nix-instantiate --parse "$file" 2>&1) || { echo "$out" >&2; return 1; }
    printf '%s\n' "$out"
}

# Every path literal in $1, as the parser resolved it (always absolute), one
# per line. A path token starts with "/" followed by a non-"/" char, so the
# "//" update operator never matches; division prints as __div and
# <nixpkgs> as __findFile, so neither yields a path either.
_refs_extract_paths() {
    local parsed
    parsed=$(_refs_parse "$1") || return 1
    _refs_strip_strings <<< "$parsed" | grep -oE '/[A-Za-z0-9._+-][A-Za-z0-9._+/-]*' || true
}

# Path literals the parser printed as the static base of a concatenation
# ("(<path> + <expr>)"): ./${x}.nix, ./. + "/..", /. + "..". The suffix is
# only known at eval time, so containment of the result can never be proven
# (#27) — always a violation. Includes the bare root "/" base, which
# _refs_extract_paths deliberately excludes (it isn't a path token by itself).
_refs_extract_dynamic_paths() {
    local parsed
    parsed=$(_refs_parse "$1") || return 1
    _refs_strip_strings <<< "$parsed" \
        | grep -oE '(^|[ ([])/([A-Za-z0-9._+-][A-Za-z0-9._+/-]*)? \+ ' \
        | grep -oE '/[^ ]*' || true
}

#──[Private: luxos.modules call-shape recognition (#28)]────────────────────

# One recognized call, as nix-instantiate --parse always prints it regardless
# of source formatting: "(luxos).modules [ ("a") ("b") ]" — a literal list of
# string-literal elements.
_REFS_CALL_SPAN_RE='\(luxos\)\.modules \[([^][]*)\]'

# Strip the outer lambda's formals declaration ("{ a, b, luxos, ... }:") from
# the start of a parse — the one place "luxos" is legitimately a bare
# identifier with no call attached (it's just the argument name).
_refs_strip_formals() {
    local parsed="$1"
    if [[ "$parsed" =~ ^(\(+)(\{[^{}]*\}:)(.*)$ ]]; then
        printf '%s' "${BASH_REMATCH[3]}"
    else
        printf '%s' "$parsed"
    fi
}

# Scan $1 (a file's parse output, formals already stripped) for every
# recognized luxos.modules call, emitting "NAME<TAB>name" for each string
# literal found inside one. Any other reference to "luxos" left over once
# every recognized call has been removed from the text is a call-shape
# violation (#28: luxos.modules with a non-literal-list argument, `with
# luxos;`, `args.luxos.modules`, ...) and is emitted as "VIOLATION<TAB>msg".
# Never aborts; refs::names (the public, aborting wrapper) and refs::validate
# (which must keep going past one file's violation, item 15) both build on
# this.
_refs_scan_luxos_uses() {
    local body="$1" rest inner tok outer_match item_match
    rest="$body"
    while [[ "$rest" =~ $_REFS_CALL_SPAN_RE ]]; do
        outer_match="${BASH_REMATCH[0]}"
        inner="${BASH_REMATCH[1]}"
        if ! [[ "$inner" =~ ^([[:space:]]*\(\"[^\"]*\"\))*[[:space:]]*$ ]]; then
            echo -e "VIOLATION\tluxos.modules is not called with a literal list of string literals"
        else
            while [[ "$inner" =~ \(\"([^\"]*)\"\) ]]; do
                tok="${BASH_REMATCH[1]}"
                item_match="${BASH_REMATCH[0]}"
                echo -e "NAME\t$tok"
                inner="${inner/"$item_match"/}"
            done
        fi
        rest="${rest/"$outer_match"/}"
    done

    # Path literals print unquoted and absolute (e.g.
    # /home/luar/.config/luxos/modules/...) — CONFIG_DIR itself can
    # legitimately contain the substring "luxos" in its own directory name,
    # so path tokens are stripped before the final leftover check below;
    # otherwise every file's own resolved path would falsely look like a use
    # of the "luxos" identifier.
    local rest_no_paths
    rest_no_paths=$(sed -E 's#/[A-Za-z0-9._+-][A-Za-z0-9._+/-]*##g' <<< "$rest")
    if [[ "$rest_no_paths" == *luxos* ]]; then
        echo -e "VIOLATION\tuses 'luxos' outside a recognized 'luxos.modules [ ... ]' call"
    fi
}

# "NAME<TAB>name" / "VIOLATION<TAB>msg" rows for $1 (absolute file path).
# Never aborts.
_refs_names_raw() {
    local parsed
    if ! parsed=$(_refs_parse "$1" 2>&1); then
        echo -e "VIOLATION\tfailed to parse: $(tail -1 <<< "$parsed")"
        return 0
    fi
    local body
    body=$(_refs_strip_formals "$parsed")
    _refs_scan_luxos_uses "$body"
}

#──[Private: owner directory (#25)]─────────────────────────────────────────

# Owning folder-module directory of $1 (absolute path, may be a file or a
# directory), or empty when none exists — the nearest ancestor strictly below
# $CONFIG_DIR/$CONFIGGEN_MODULES_DIR that has its own default.nix. Comparison
# is purely lexical (string prefix, per §3), so walking through the CONFIG_DIR
# symlink or its real path gives identical results — never resolved with
# readlink/realpath.
_refs_owner_dir() {
    local file="$1" root="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR" d
    d=$(dirname "$file")
    while [[ "$d" == "$root"/* ]]; do
        if [[ -f "$d/default.nix" ]]; then
            echo "$d"
            return 0
        fi
        d=$(dirname "$d")
    done
}

#──[Public API]──────────────────────────────────────────────────────────────

# Owning folder-module directory of $1 (absolute path), or empty for a
# single-file module. See _refs_owner_dir.
refs::owner() {
    _refs_owner_dir "$1"
}

# Every static path literal referenced by $1 (absolute path), one per line,
# always absolute (as the parser resolved it). Hard error (exit 1) if $1
# fails to parse.
refs::paths() {
    local file="$1"
    _refs_extract_paths "$file" || exit 1
}

# Every dynamic-path base (#27) referenced by $1 (absolute path), one per
# line. Hard error (exit 1) if $1 fails to parse.
refs::dynamic_paths() {
    local file="$1"
    _refs_extract_dynamic_paths "$file" || exit 1
}

# Names from every recognized "luxos.modules [ ... ]" call in $1 (absolute
# path), one per line. Hard error (exit 1), naming the file, on any other use
# of "luxos" (#28) or on a parse failure.
refs::names() {
    local file="$1" rows kind val
    rows=$(_refs_names_raw "$file")
    while IFS=$'\t' read -r kind val; do
        [[ -n "$kind" ]] || continue
        if [[ "$kind" == "VIOLATION" ]]; then
            echo "Error: $file: $val" >&2
            exit 1
        fi
    done <<< "$rows"
    while IFS=$'\t' read -r kind val; do
        [[ "$kind" == "NAME" ]] && printf '%s\n' "$val"
    done <<< "$rows"
    return 0
}

# Walk every *.nix file under $CONFIG_DIR/$CONFIGGEN_MODULES_DIR (following
# the modules/system symlink — framework modules obey the same rule, #31),
# except the root's own default.nix (the host-selection mirror, exempt per
# §3). Collects every boundary, dynamic-path, call-shape and unresolved-name
# violation across the WHOLE tree (item 15 — not only the first), prints them
# all, then exits 1 if any were found. Silent on success.
refs::validate() {
    local root="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR"
    local -a violations=()
    local file rel owner p kind val name

    while IFS= read -r file; do
        [[ "$file" == "$root/default.nix" ]] && continue
        rel="${file#"$root"/}"
        owner=$(_refs_owner_dir "$file")

        local static_out dynamic_out parsed_ok=true
        if static_out=$(_refs_extract_paths "$file"); then
            while IFS= read -r p; do
                [[ -n "$p" ]] || continue
                if [[ -n "$owner" && ( "$p" == "$owner" || "$p" == "$owner"/* ) ]]; then continue; fi
                violations+=("Error: $rel: references $p outside its module")
                violations+=("  Move the file into the module, or use luxos.modules.")
            done <<< "$static_out"
        else
            parsed_ok=false
        fi

        if $parsed_ok && dynamic_out=$(_refs_extract_dynamic_paths "$file"); then
            while IFS= read -r p; do
                [[ -n "$p" ]] || continue
                violations+=("Error: $rel: uses a dynamic path ($p)")
                violations+=("  Move the file into the module, or use luxos.modules.")
            done <<< "$dynamic_out"
        elif ! $parsed_ok; then
            :
        else
            parsed_ok=false
        fi

        if ! $parsed_ok; then
            violations+=("Error: $rel: failed to parse")
        fi

        while IFS=$'\t' read -r kind val; do
            [[ -n "$kind" ]] || continue
            case "$kind" in
                VIOLATION)
                    violations+=("Error: $rel: $val")
                    ;;
                NAME)
                    if ! configgen::resolve_name "$val" > /dev/null; then
                        violations+=("Error: $rel: luxos.modules: '$val' does not resolve to any module under $CONFIGGEN_MODULES_DIR/")
                    fi
                    ;;
            esac
        done < <(_refs_names_raw "$file")
    done < <(find -L "$root" -type f -name '*.nix' | LC_ALL=C sort)

    if [[ ${#violations[@]} -gt 0 ]]; then
        printf '%s\n' "${violations[@]}" >&2
        exit 1
    fi
}

# Module files under $CONFIG_DIR/$CONFIGGEN_MODULES_DIR whose luxos.modules
# list contains $1. Read-only; a file whose luxos.modules use isn't the
# recognized shape is silently skipped (same tolerance as imports::importers
# for a host entrypoint it can't parse — it just can't be a hit).
refs::dependents() {
    local name="$1" root="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR"
    local file rows kind val hit

    while IFS= read -r file; do
        [[ "$file" == "$root/default.nix" ]] && continue
        rows=$(_refs_names_raw "$file") || continue
        hit=false
        while IFS=$'\t' read -r kind val; do
            [[ "$kind" == "NAME" && "$val" == "$name" ]] && { hit=true; break; }
        done <<< "$rows"
        $hit && echo "$file"
    done < <(find -L "$root" -type f -name '*.nix' | LC_ALL=C sort)
    return 0
}

#──[Private: retarget's source-level rewrite]───────────────────────────────

# A "luxos.modules [ ... ]" call as it looks in hand-written SOURCE (any
# whitespace after "modules", no parens around "luxos" — nix-instantiate
# --parse is what normalizes it to "(luxos).modules [...]", not the file on
# disk). Item form: a plain quoted string, "name".
_REFS_SOURCE_SPAN_RE='luxos\.modules[[:space:]]*\[([^][]*)\]'
_REFS_SOURCE_ITEM_RE='"([^"]*)"'

# The same call as nix-instantiate --parse always prints it, regardless of
# source formatting (see _REFS_CALL_SPAN_RE above). Item form: a
# parenthesized quoted string, ("name").
_REFS_PARSE_ITEM_RE='\("([^"]*)"\)'

# Shared body of _refs_rewrite_source/_refs_rewrite_parse: rewrite every
# matching "luxos.modules [ ... ]" span in $1 that contains the literal
# element $2 ($old) — replacing it with $3 ($new) if given, else dropping it
# — using the span regex $4, the item regex $5, and the item-quoting style $6
# ("plain" -> "tok", "parens" -> ("tok")). Spans with no occurrence of $old
# are left byte-for-byte, including their original formatting. Prints the
# rewritten text; returns 1 (text unchanged) if $old wasn't found in any
# span.
_refs_rewrite_calls() {
    local text="$1" old="$2" new="$3" span_re="$4" item_re="$5" style="$6"
    local out="" rest="$text" span inner tok item_match changed=false
    local -a items
    local needle
    [[ "$style" == "parens" ]] && needle="(\"$old\")" || needle="\"$old\""
    while [[ "$rest" =~ $span_re ]]; do
        span="${BASH_REMATCH[0]}"
        out+="${rest%%"$span"*}"
        if [[ "$span" == *"$needle"* ]]; then
            changed=true
            inner="${BASH_REMATCH[1]}"
            items=()
            while [[ "$inner" =~ $item_re ]]; do
                tok="${BASH_REMATCH[1]}"
                item_match="${BASH_REMATCH[0]}"
                if [[ "$style" == "parens" ]]; then
                    [[ "$tok" == "$old" ]] || items+=("(\"$tok\")")
                    [[ "$tok" == "$old" && -n "$new" ]] && items+=("(\"$new\")")
                else
                    [[ "$tok" == "$old" ]] || items+=("\"$tok\"")
                    [[ "$tok" == "$old" && -n "$new" ]] && items+=("\"$new\"")
                fi
                inner="${inner/"$item_match"/}"
            done
            local head
            [[ "$style" == "parens" ]] && head="(luxos).modules" || head="luxos.modules"
            if ((${#items[@]})); then
                local joined
                joined=$(printf ' %s' "${items[@]}")
                out+="$head [$joined ]"
            else
                out+="$head [ ]"
            fi
        else
            out+="$span"
        fi
        rest="${rest#*"$span"}"
    done
    out+="$rest"

    $changed || return 1
    printf '%s' "$out"
}

# Rewrite every "luxos.modules [ ... ]" span in $1 (SOURCE text, hand
# formatting) that contains the literal element "$2" ($old): to "$3" ($new)
# if given, else drop the element. See _refs_rewrite_calls.
_refs_rewrite_source() {
    _refs_rewrite_calls "$1" "$2" "${3:-}" "$_REFS_SOURCE_SPAN_RE" "$_REFS_SOURCE_ITEM_RE" plain
}

# The same rewrite, applied to a file's PARSE output instead of its source —
# used to compute the expected new parse independently of the source-level
# rewrite above, so the two can be cross-checked (§3 "Rewrite").
_refs_rewrite_parse() {
    _refs_rewrite_calls "$1" "$2" "${3:-}" "$_REFS_CALL_SPAN_RE" "$_REFS_PARSE_ITEM_RE" parens
}

#──[Public API: the one writer]─────────────────────────────────────────────

# The single writer of module files (#30, §3 "Rewrite"): in every module file
# under $CONFIG_DIR/$CONFIGGEN_MODULES_DIR whose luxos.modules list contains
# $1 ($name), rewrite "$1" to "$2" ($new_name) if given, else delete it from
# the list. Prints each change; a file with no occurrence is left untouched.
#
# Safety: rewrites happen in the SOURCE file, never in parse output. Before
# writing, the candidate is re-parsed and required to equal the ORIGINAL
# parse with exactly that one substitution applied (computed independently,
# by applying the identical rewrite to the parse text) — any other
# difference refuses the write and names the file. Same write discipline as
# the imports package: temp file then `cat >` (never `mv`).
#
# Each dependent's call shape is validated (refs::names, which hard-errors on
# an unrecognized use of luxos) before any file is touched, so a bad shape
# anywhere refuses the whole operation before anything changes (item 20).
refs::retarget() {
    local name="$1" new_name="${2:-}"
    local -a files
    mapfile -t files < <(refs::dependents "$name")
    [[ ${#files[@]} -eq 0 ]] && return 0

    # Validate shape everywhere first — refs::names aborts naming the file.
    local file
    for file in "${files[@]}"; do
        refs::names "$file" > /dev/null
    done

    if ! command -v nix-instantiate > /dev/null 2>&1; then
        echo "Error: nix-instantiate not found — required to validate module files before writing" >&2
        exit 1
    fi

    for file in "${files[@]}"; do
        local src original_parse expected_parse new_src actual_parse
        src=$(cat "$file")
        original_parse=$(_refs_parse "$file") || {
            echo "Error: $file failed to parse" >&2
            exit 1
        }

        new_src=$(_refs_rewrite_source "$src" "$name" "$new_name") || continue
        expected_parse=$(_refs_rewrite_parse "$original_parse" "$name" "$new_name") || continue

        local tmp
        tmp=$(mktemp)
        printf '%s\n' "$new_src" > "$tmp"

        actual_parse=$(_refs_parse "$tmp" 2>/dev/null) || {
            echo "Error: $file: rewrite would fail to parse — refusing to write" >&2
            rm -f "$tmp"
            exit 1
        }

        if [[ "$actual_parse" != "$expected_parse" ]]; then
            echo "Error: $file: rewrite would change more than the '$name' reference — refusing to write" >&2
            rm -f "$tmp"
            exit 1
        fi

        cat "$tmp" > "$file"
        rm -f "$tmp"

        if [[ -n "$new_name" ]]; then
            echo "$file: \"$name\" -> \"$new_name\""
        else
            echo "$file: removed \"$name\""
        fi
    done
    return 0
}
