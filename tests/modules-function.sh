#!/usr/bin/env bash
# Suite: a name-resolving `luxos.modules` inside the real Nix module system.
# The name map is built by the framework's own configgen::walk_units, over a
# fixture laid out like $STAGING_DIR.
set -uo pipefail
TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$TESTS_DIR/lib.sh"

FRAMEWORK_CONFIGGEN="$TESTS_DIR/../internal/configgen/configgen.sh"
NIXPKGS_LIB="<nixpkgs/lib>"
NIXPKGS_PATH=$(nix-instantiate --eval -E '<nixpkgs>' | tr -d '"')

F=$(new_fixture)
trap 'rm -rf "$F"' EXIT
S="$F/staged"
M="$S/config/modules"

# A shared option every fixture module appends its own name to, so a test can
# count how many times a module body actually ran.
write "$S/marks.nix" '{ lib, ... }: { options.marks = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; }; }'

write "$M/system/desktop.nix"                '{ config.marks = [ "desktop" ]; }'
write "$M/system/wayland.nix"                '{ luxos, lib, ... }: { imports = luxos.modules [ "desktop" ]; options.waylandOpt = lib.mkOption { default = 1; }; config.marks = [ "wayland" ]; }'
write "$M/desktop/compositors/hyprland.nix"  '{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; config.marks = [ "hyprland" ]; }'
write "$M/desktop/greeters/ly-greeter.nix"   '{ config.marks = [ "ly-greeter" ]; }'
write "$M/users/luar/default.nix"            '{ imports = [ ./account.nix ]; config.marks = [ "luar" ]; }'
write "$M/users/luar/account.nix"            '{ config.marks = [ "account" ]; }'
write "$M/cyc/a.nix"                         '{ luxos, ... }: { imports = luxos.modules [ "b" ]; config.marks = [ "a" ]; }'
write "$M/cyc/b.nix"                         '{ luxos, ... }: { imports = luxos.modules [ "a" ]; config.marks = [ "b" ]; }'

# Emit the name map from the real walker: { "name" = ./config/modules/<path>; }
units=$(CONFIG_DIR="$S/config" bash -c "source '$FRAMEWORK_CONFIGGEN' && configgen::walk_units") \
    || { echo "walk_units failed"; exit 1; }
{
    echo "{"
    while IFS=$'\t' read -r name path; do
        printf '  "%s" = ./config/modules/%s;\n' "$name" "$path"
    done <<< "$units"
    echo "}"
} > "$S/units.nix"

# The whole proposed function: lookup with a loud failure, nothing else.
write "$S/luxos.nix" '{ units }: {
  modules = names: map (n: units.${n} or (throw "luxos.modules: '"'"'${n}'"'"' does not resolve to any module under modules/")) names;
}'

section "generated name map (from configgen::walk_units)"
awk '{print "    " $0}' "$S/units.nix"

# ev <nix expr evaluated with $S as cwd> — prints JSON or the error, returns nix status.
ev() {
    (cd "$S" && nix-instantiate --eval --strict --json -E "$1" 2>&1)
}

PRELUDE="let lib = import $NIXPKGS_LIB; luxos = import ./luxos.nix { units = import ./units.nix; };
  eval = mods: lib.evalModules { specialArgs = { inherit luxos; }; modules = [ ./marks.nix ] ++ mods; }; in"

# expect_json <label> <expr> <expected json>
expect_json() {
    local got
    got=$(ev "$PRELUDE $2")
    [[ "$got" == "$3" ]] && pass "$1 -> $got" || fail "$1" "expected $3, got: $(tail -3 <<< "$got" | tr '\n' ' ')"
}
# expect_error <label> <expr> <regex the error must match>
expect_error() {
    local got
    if got=$(ev "$PRELUDE $2"); then
        fail "$1" "expected an error, evaluated to: $got"
    elif grep -qE "$3" <<< "$got"; then
        pass "$1 -> $(grep -oE "$3" <<< "$got" | head -1)"
    else
        fail "$1" "error didn't match /$3/: $(grep -m1 'error:' <<< "$got")"
    fi
}

section "resolution via specialArgs"
expect_json  "import by name (transitive: hyprland -> wayland -> desktop)" \
    '(eval [ ./config/modules/desktop/compositors/hyprland.nix ]).config.marks' \
    '["desktop","wayland","hyprland"]'
expect_json  "folder module resolves to its directory" \
    '(eval [ ({ luxos, ... }: { imports = luxos.modules [ "luar" ]; }) ]).config.marks' \
    '["account","luar"]'
expect_json  "name with a dash" \
    '(eval [ ({ luxos, ... }: { imports = luxos.modules [ "ly-greeter" ]; }) ]).config.marks' \
    '["ly-greeter"]'
expect_error "unknown name fails loudly" \
    '(eval [ ({ luxos, ... }: { imports = luxos.modules [ "waylnd" ]; }) ]).config.marks' \
    "luxos.modules: 'waylnd' does not resolve to any module under modules/"
expect_error "a file-inside-a-folder-module is not a name" \
    '(eval [ ({ luxos, ... }: { imports = luxos.modules [ "account" ]; }) ]).config.marks' \
    "'account' does not resolve"

section "_module.args instead of specialArgs"
expect_error "imports can't see _module.args" \
    'let lib = import <nixpkgs/lib>; in (lib.evalModules { modules = [ ./marks.nix { _module.args.luxos = luxos; } ./config/modules/desktop/compositors/hyprland.nix ]; }).config.marks' \
    "infinite recursion|attribute 'luxos' missing"

section "dedup: selected by path AND pulled in by name (order-insensitive)"
SORTED='builtins.sort builtins.lessThan'
expect_json  "same module twice runs once" \
    "$SORTED (eval [ ./config/modules/system/wayland.nix ./config/modules/desktop/compositors/hyprland.nix ]).config.marks" \
    '["desktop","hyprland","wayland"]'
expect_json  "lexically different spelling of the same path still dedupes" \
    "$SORTED (eval [ ./config/modules/desktop/../system/wayland.nix ./config/modules/desktop/compositors/hyprland.nix ]).config.marks" \
    '["desktop","hyprland","wayland"]'

section "cycles are a native module-system limit, not introduced by the function"
write "$M/cyc/p.nix" '{ imports = [ ./q.nix ]; config.marks = [ "p" ]; }'
write "$M/cyc/q.nix" '{ imports = [ ./p.nix ]; config.marks = [ "q" ]; }'
expect_error "plain-path cycle p <-> q overflows" \
    '(eval [ ./config/modules/cyc/p.nix ]).config.marks' \
    "max-call-depth exceeded"
expect_error "name cycle a <-> b overflows identically" \
    '(eval [ ./config/modules/cyc/a.nix ]).config.marks' \
    "max-call-depth exceeded"

section "dedup key is the path spelling: a symlinked spelling runs twice, silently"
ln -s "$M/system" "$M/syslink"
expect_json  "wayland via syslink/ + via name -> body runs twice, no error" \
    "$SORTED (eval [ ./config/modules/syslink/wayland.nix ./config/modules/desktop/compositors/hyprland.nix ]).config.marks" \
    '["desktop","hyprland","wayland","wayland"]'
rm "$M/syslink"

section "module that needs luxos, evaluated without it (--bypass shape)"
expect_error "missing specialArg names the argument" \
    'let lib = import <nixpkgs/lib>; in (lib.evalModules { modules = [ ./marks.nix ./config/modules/desktop/compositors/hyprland.nix ]; }).config.marks' \
    "luxos"

section "pure flake eval, staged shape (entrypoint at config/modules, map in flake.nix)"
write "$M/default.nix" '{ imports = [ ./system/wayland.nix ./desktop/compositors/hyprland.nix ]; }'
cat > "$S/flake.nix" <<EOF
{
  inputs.nixpkgs.url = "path:$NIXPKGS_PATH";
  outputs = { nixpkgs, ... }:
  let
    lib = nixpkgs.lib;
    luxos = import ./luxos.nix { units = import ./units.nix; };
  in {
    marks = builtins.sort builtins.lessThan (lib.evalModules {
      specialArgs = { inherit luxos; };
      modules = [ ./marks.nix ./config/modules ];
    }).config.marks;
  };
}
EOF
got=$(cd "$S" && nix eval --json --no-write-lock-file "path:$S#marks" 2>/dev/null)
[[ "$got" == '["desktop","hyprland","wayland"]' ]] \
    && pass "flake: entrypoint path + name import dedupe in the store -> $got" \
    || fail "flake eval" "$(tail -3 <<< "$got" | tr '\n' ' ')"

summary
