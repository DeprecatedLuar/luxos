#!/usr/bin/env bash
# Suite: the module-boundary rule (refs::validate, internal/refs/refs.sh)
# against a fixture tree shaped like the real CONFIG_DIR (config dir itself a
# symlink, modules/system a symlink into the framework).
#
# Rule under test (implementation-plan.md #25, #27, §3 "Module boundary"):
#   - A file's module is its nearest ancestor directory (below modules/) that
#     has a default.nix. With none, the file is a single-file module.
#   - Every path a file references must be inside its folder module.
#     A single-file module may reference no paths at all.
#   - Dynamic paths (./${x}.nix, ./. + "...") are unprovable: always rejected.
#   - modules/default.nix (the host selection mirror) is exempt.
#   - Every path counts, not just .nix files (#25) — refs::validate always
#     checks everything; there is no narrower "nix files only" mode.
set -uo pipefail
TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$TESTS_DIR/lib.sh"
source "$TESTS_DIR/../internal/env.sh"
source "$TESTS_DIR/../internal/refs/refs.sh"

# violations <config_dir> — runs refs::validate with CONFIG_DIR set to $1,
# prints "relfile<TAB>kind" for every violation found (parsed back out of its
# stderr), one per line. refs::validate exits 1 whenever it found anything,
# so its own exit status isn't itself informative here — the parsed rows are.
violations() {
    local cfg="$1" err
    err=$(CONFIG_DIR="$cfg" refs::validate 2>&1 1>/dev/null) || true
    grep -oE '^Error: [^:]+: (references|uses a dynamic path)' <<< "$err" \
        | awk -F': ' '{ kind = ($3 == "references") ? "outside" : "dynamic"; print $2"\t"kind }'
}

F=$(new_fixture)
trap 'rm -rf "$F"' EXIT

# Layout: $F/cfg -> $F/real (like ~/.config/luxos), real/modules/system -> $F/fw/mods
mkdir -p "$F/real/modules" "$F/fw/mods"
ln -s "$F/real" "$F/cfg"
ln -s "$F/fw/mods" "$F/real/modules/system"
M="$F/real/modules"
FW="$F/fw/mods"

write "$M/default.nix"                          '{ imports = [ ./desktop/compositors/hyprland.nix ../elsewhere.nix ]; }'
write "$FW/desktop.nix"                         '{ }'
write "$FW/wayland.nix"                         '{ imports = [ ./desktop.nix ]; }'
write "$FW/bundle/default.nix"                  '{ imports = [ ./parts.nix ]; }'
write "$FW/bundle/parts.nix"                    '{ }'
write "$M/desktop/compositors/hyprland.nix"     '{ imports = [ ../../system/wayland.nix ]; }'
write "$M/desktop/compositors/niri.nix"         '{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }'
write "$M/desktop/shells/ambxst.nix"            '{ inputs, ... }: { imports = [ inputs.ambxst.nixosModules.default ]; }'
write "$M/users/luar/default.nix"               '{ imports = [ ./account.nix ./nested/deep.nix ]; }'
write "$M/users/luar/account.nix"               '{ x = builtins.readFile ./key.pub; }'
write "$M/users/luar/key.pub"                   'ssh-ed25519 AAAA'
write "$M/users/luar/nested/deep.nix"          '{ imports = [ ../account.nix ]; y = ./.; }'
write "$M/users/luar/leak.nix"                  '{ imports = [ ../../desktop/shells/ambxst.nix ]; }'
write "$M/users/bob/default.nix"                '{ imports = [ ../luar/account.nix ]; }'
write "$M/foo/default.nix"                      '{ imports = [ ../foobar/x.nix ]; }'
write "$M/foobar/x.nix"                         '{ }'
write "$M/theme/single.nix"                     '{ x = builtins.readFile ../wall.png; }'
write "$M/theme/uses-dir.nix"                   '{ imports = [ ../foo ]; }'
write "$M/dyn/default.nix"                      'let n = "x"; in { imports = [ ./${n}.nix ]; }'
write "$M/abs.nix"                              '{ imports = [ /etc/nixos/x.nix ]; }'
write "$M/_hidden.nix"                          '{ }'
write "$M/hidden-user.nix"                      '{ imports = [ ./_hidden.nix ]; }'

# relfile<TAB>kind — the exact violation set expected. Every path counts
# (#25), so there is no narrower scope to test separately any more — this is
# the one and only set refs::validate ever produces for this fixture.
EXPECTED=$(cat <<'EOF'
abs.nix	outside
desktop/compositors/hyprland.nix	outside
dyn/default.nix	dynamic
foo/default.nix	outside
hidden-user.nix	outside
system/wayland.nix	outside
theme/single.nix	outside
theme/uses-dir.nix	outside
users/bob/default.nix	outside
users/luar/leak.nix	outside
EOF
)
EXPECTED=$(LC_ALL=C sort -u <<< "$EXPECTED")

section "walked through the config symlink ($F/cfg/modules)"
got_full=$(violations "$F/cfg")
printf '%s\n' "$got_full" | awk -F'\t' 'NF{printf "    %-36s %s\n", $1, $2}'
got=$(LC_ALL=C sort -u <<< "$got_full")

if [[ "$got" == "$EXPECTED" ]]; then
    pass "violation set matches exactly"
else
    fail "violation set differs" "$(diff <(echo "$EXPECTED") <(echo "$got") | grep '^[<>]' | tr '\n' ';')"
fi

section "clean files produce no violations"
for f in system/desktop.nix system/bundle/default.nix system/bundle/parts.nix \
         desktop/compositors/niri.nix desktop/shells/ambxst.nix \
         users/luar/default.nix users/luar/account.nix users/luar/nested/deep.nix foobar/x.nix; do
    if grep -q "^$f"$'\t' <<< "$got_full"; then
        fail "$f flagged"
    else
        pass "$f clean"
    fi
done

section "same result walking the physical path"
a=$(violations "$F/cfg")
b=$(violations "$F/real")
[[ "$a" == "$b" ]] && pass "config symlink vs real path agree" || fail "results differ" "$(diff <(echo "$a") <(echo "$b") | tr '\n' ';')"

section "refs::validate exit status"
# refs::validate aborts the process (exit 1) on a violation, matching every
# other internal/*.sh package's convention — run it in an explicit subshell
# so that exit only ends the subshell, not this test script.
if (CONFIG_DIR="$F/cfg" refs::validate) 2>/dev/null; then
    fail "should exit 1 with violations present"
else
    pass "exits 1 with violations present"
fi

# A clean subtree (no violations) exits 0 and prints nothing.
CLEAN=$(new_fixture)
trap 'rm -rf "$F" "$CLEAN"' EXIT
mkdir -p "$CLEAN/modules"
write "$CLEAN/modules/default.nix" '{ imports = [ ./a.nix ]; }'
write "$CLEAN/modules/a.nix"       '{ }'
clean_err=$( (CONFIG_DIR="$CLEAN" refs::validate) 2>&1 )
clean_status=$?
if [[ $clean_status -eq 0 && -z "$clean_err" ]]; then
    pass "clean tree: exits 0, silent"
else
    fail "clean tree should exit 0 and print nothing" "status=$clean_status output=[$clean_err]"
fi

summary
