#!/usr/bin/env bash
# Suite: luxos.modules call-shape recognition (implementation-plan.md #28,
# §3 "luxos.modules"), via the real internal/refs/refs.sh package
# (refs::names). The only recognized shape is a literal list of string
# literals; every other use of "luxos" beyond the function's own formals is a
# hard error.
set -uo pipefail
TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$TESTS_DIR/lib.sh"
source "$TESTS_DIR/../internal/env.sh"
source "$TESTS_DIR/../internal/refs/refs.sh"

F=$(new_fixture)
trap 'rm -rf "$F"' EXIT
D="$F/cat"

# ok <label> <nix-body> <expected names, newline separated, may be empty>
ok() {
    local label="$1" body="$2" expected="$3" file="$D/case.nix" got
    write "$file" "$body"
    got=$(refs::names "$file" 2>&1)
    expected=$(printf '%s' "$expected" | LC_ALL=C sort -u)
    got_sorted=$(LC_ALL=C sort -u <<< "$got")
    if [[ "$got_sorted" == "$expected" ]]; then
        pass "$label"
    else
        fail "$label" "expected [$(tr '\n' ' ' <<< "$expected")] got [$(tr '\n' ' ' <<< "$got")]"
    fi
}

# bad <label> <nix-body> <regex the stderr message must match>
bad() {
    local label="$1" body="$2" pattern="$3" file="$D/case.nix" out status
    write "$file" "$body"
    # refs::names hard-errors (exit 1) on a violation, matching every other
    # internal/*.sh package's convention — the "if" here keeps that exit from
    # tripping this test script's own errexit (refs.sh sets it on source).
    if out=$( (refs::names "$file") 2>&1 ); then
        status=0
    else
        status=1
    fi
    if [[ $status -eq 0 ]]; then
        fail "$label" "expected a hard error, got: $out"
    elif grep -qE "$pattern" <<< "$out"; then
        pass "$label -> $(grep -oE "$pattern" <<< "$out" | head -1)"
    else
        fail "$label" "error didn't match /$pattern/: $out"
    fi
}

section "recognized shape: literal list of string literals"
ok "single name"           '{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }'          "wayland"
ok "multiple names"        '{ luxos, ... }: { imports = luxos.modules [ "wayland" "x11" ]; }'     $'wayland\nx11'
ok "empty list"            '{ luxos, ... }: { imports = luxos.modules [ ]; }'                     ""
ok "dashed name"           '{ luxos, ... }: { imports = luxos.modules [ "ly-greeter" ]; }'        "ly-greeter"
ok "not used at all"       '{ ... }: { imports = [ ./a.nix ]; }'                                  ""
ok "two separate calls"    '{ luxos, ... }: { a = luxos.modules [ "x" ]; b = luxos.modules [ "y" ]; }' $'x\ny'
ok "formals with other args" '{ config, lib, luxos, ... }: { imports = luxos.modules [ "a" ]; }'  "a"
ok "luxos.modules not in imports" '{ luxos, ... }: { x = luxos.modules [ "a" ]; }'                "a"

section "hard errors: any other use of luxos (#28)"
# An undeclared "names" is a free variable nix-instantiate --parse itself
# rejects (it resolves scoping, not just syntax) before refs.sh's own
# call-shape grammar ever runs — still a hard error naming the file, just via
# the parse-failure path rather than the "outside a recognized call" one.
bad "non-literal-list argument (bare undeclared variable)" \
    '{ luxos, ... }: { imports = luxos.modules names; }' \
    "failed to parse"
bad "non-literal-list argument (bare string, no list)" \
    '{ luxos, ... }: { imports = luxos.modules "a"; }' \
    "outside a recognized"
bad "non-literal-list argument (declared variable, #28's own example)" \
    '{ luxos, ... }: let names = [ "a" ]; in { imports = luxos.modules names; }' \
    "outside a recognized"
bad "non-string-literal list element" \
    '{ luxos, ... }: let a = "x"; in { imports = luxos.modules [ a ]; }' \
    "literal list of string literals"
bad "with luxos;" \
    '{ luxos, ... }: with luxos; { imports = modules [ "a" ]; }' \
    "outside a recognized"
bad "args.luxos.modules" \
    '{ args, ... }: { imports = args.luxos.modules [ "a" ]; }' \
    "outside a recognized"
bad "luxos referenced bare" \
    '{ luxos, ... }: { x = luxos; }' \
    "outside a recognized"
bad "luxos.modules with list concat argument" \
    '{ luxos, ... }: { imports = luxos.modules ([ "a" ] ++ [ "b" ]); }' \
    "outside a recognized"

section "parse failure is a hard error too"
bad "syntactically invalid file" \
    '{ luxos, ... }: { imports = [ ; }' \
    "."

summary
