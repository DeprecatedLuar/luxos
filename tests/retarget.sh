#!/usr/bin/env bash
# Suite: refs::retarget (implementation-plan.md #30, §3 "Rewrite") — the one
# writer of module files. Rewrites or deletes the literal "old" string inside
# every dependent's luxos.modules [ ... ] call, in the SOURCE file, and only
# commits the write once the candidate re-parses to exactly the original
# parse with that one substitution applied.
set -uo pipefail
TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$TESTS_DIR/lib.sh"
source "$TESTS_DIR/../internal/env.sh"
source "$TESTS_DIR/../internal/refs/refs.sh"

F=$(new_fixture)
trap 'rm -rf "$F"' EXIT
mkdir -p "$F/modules"

section "rename: rewrites every dependent, leaves everything else untouched"
write "$F/modules/default.nix" '{ imports = [ ./a.nix ./b.nix ./c.nix ]; }'
write "$F/modules/a.nix" '{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }'
write "$F/modules/b.nix" $'# a comment that must survive\n{ luxos, ... }:\n{\n  imports = luxos.modules [\n    "x"\n    "wayland"\n  ];\n} # trailing too'
write "$F/modules/c.nix" '{ }' # not a dependent at all: must be left alone
write "$F/modules/wayland.nix" '{ }'

out=$( (CONFIG_DIR="$F" refs::retarget wayland wl) 2>&1 )
status=$?
if [[ $status -eq 0 ]]; then pass "retarget exits 0"; else fail "retarget should exit 0" "$out"; fi

if grep -q 'a\.nix.*"wayland" -> "wl"' <<< "$out"; then pass "a.nix change reported"; else fail "a.nix change not reported" "$out"; fi
if grep -q 'b\.nix.*"wayland" -> "wl"' <<< "$out"; then pass "b.nix change reported"; else fail "b.nix change not reported" "$out"; fi
if grep -q 'c\.nix' <<< "$out"; then fail "c.nix should not be mentioned" "$out"; else pass "c.nix untouched, unmentioned"; fi

got_a=$(cat "$F/modules/a.nix")
[[ "$got_a" == '{ luxos, ... }: { imports = luxos.modules [ "wl" ]; }' ]] \
    && pass "a.nix rewritten correctly" || fail "a.nix content wrong" "$got_a"

got_c=$(cat "$F/modules/c.nix")
[[ "$got_c" == '{ }' ]] && pass "c.nix byte-identical" || fail "c.nix changed" "$got_c"

got_b=$(cat "$F/modules/b.nix")
if grep -q 'a comment that must survive' <<< "$got_b" && grep -q 'trailing too' <<< "$got_b" \
    && grep -q '"x"' <<< "$got_b" && grep -q '"wl"' <<< "$got_b" && ! grep -q '"wayland"' <<< "$got_b"; then
    pass "b.nix: comments preserved, list content updated"
else
    fail "b.nix rewrite wrong" "$got_b"
fi

echo "$(nix-instantiate --parse "$F/modules/a.nix" > /dev/null 2>&1; echo $?)" | grep -q '^0$' \
    && pass "a.nix still parses" || fail "a.nix no longer parses"
nix-instantiate --parse "$F/modules/b.nix" > /dev/null 2>&1 \
    && pass "b.nix still parses" || fail "b.nix no longer parses"

section "remove: drops the element, deletes single-element lists cleanly"
write "$F/modules/default.nix" '{ imports = [ ./a.nix ./d.nix ]; }'
write "$F/modules/d.nix" '{ luxos, ... }: { imports = luxos.modules [ "wl" ]; }'

out=$( (CONFIG_DIR="$F" refs::retarget wl) 2>&1 )
if grep -q 'removed "wl"' <<< "$out"; then pass "remove reported"; else fail "remove not reported" "$out"; fi

got_d=$(cat "$F/modules/d.nix")
[[ "$got_d" == '{ luxos, ... }: { imports = luxos.modules [ ]; }' ]] \
    && pass "d.nix: list emptied cleanly" || fail "d.nix content wrong" "$got_d"
nix-instantiate --parse "$F/modules/d.nix" > /dev/null 2>&1 \
    && pass "d.nix still parses" || fail "d.nix no longer parses"

section "no dependents: no-op, no error"
if out=$( (CONFIG_DIR="$F" refs::retarget nonexistent-name newname) 2>&1 ); then
    [[ -z "$out" ]] && pass "no dependents: silent, exit 0" || fail "expected silence" "$out"
else
    fail "no dependents should exit 0"
fi

section "item 20: a dependent with an unrecognized luxos shape refuses the whole write"
write "$F/modules/default.nix" '{ imports = [ ./e.nix ./f.nix ]; }'
write "$F/modules/e.nix" '{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }'
write "$F/modules/f.nix" '{ luxos, ... }: { imports = luxos.modules [ "shared" ]; extra = luxos; }'
before_e=$(cat "$F/modules/e.nix")
before_f=$(cat "$F/modules/f.nix")

if out=$( (CONFIG_DIR="$F" refs::retarget shared renamed) 2>&1 ); then
    fail "retarget should refuse when a dependent's shape is unrecognized" "$out"
else
    pass "retarget refuses -> $(tail -1 <<< "$out")"
fi
[[ "$(cat "$F/modules/e.nix")" == "$before_e" ]] && pass "e.nix untouched after refusal" || fail "e.nix was written despite refusal"
[[ "$(cat "$F/modules/f.nix")" == "$before_f" ]] && pass "f.nix untouched after refusal" || fail "f.nix was written despite refusal"

section "idempotent: a second identical retarget changes nothing further"
write "$F/modules/default.nix" '{ imports = [ ./g.nix ]; }'
write "$F/modules/g.nix" '{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }'
rm -f "$F/modules/e.nix" "$F/modules/f.nix"
CONFIG_DIR="$F" refs::retarget shared renamed > /dev/null
before=$(cat "$F/modules/g.nix")
out2=$( (CONFIG_DIR="$F" refs::retarget shared renamed) 2>&1 )
after=$(cat "$F/modules/g.nix")
[[ -z "$out2" && "$before" == "$after" ]] \
    && pass "second run of an already-applied rename is a silent no-op" \
    || fail "second run should be a no-op" "out=[$out2]"

summary
