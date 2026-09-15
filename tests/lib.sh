#!/usr/bin/env bash
# Shared assert + fixture helpers for tests/*.sh. Sourced, not executed.

PASS_COUNT=0
FAIL_COUNT=0

pass() { PASS_COUNT=$((PASS_COUNT + 1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; [[ -n "${2:-}" ]] && printf '       %s\n' "$2"; }

section() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

summary() {
    printf '\n%d passed, %d failed\n' "$PASS_COUNT" "$FAIL_COUNT"
    [[ $FAIL_COUNT -eq 0 ]]
}

# write <path> <content> — creates parent dirs.
write() {
    mkdir -p "$(dirname "$1")"
    printf '%s\n' "$2" > "$1"
}

new_fixture() {
    mktemp -d "${TMPDIR:-/tmp}/modtests.XXXXXX"
}

# Path extraction, string stripping and module-boundary checking used to be
# prototyped here (extract_paths/dynamic_paths/strip_strings) and in
# boundary.sh (check_modules). That logic now lives for real in
# internal/refs/refs.sh (refs::paths/refs::dynamic_paths/refs::owner/
# refs::validate) — suites that need it source internal/env.sh and
# internal/refs/refs.sh directly instead.
