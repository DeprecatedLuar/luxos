#!/usr/bin/env bash
# Suite: what `nix-instantiate --parse` exposes about paths, via the real
# internal/refs/refs.sh package (refs::paths / refs::dynamic_paths). Every
# case is one .nix file; the extracted path set is compared against the
# expectation.
set -uo pipefail
TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$TESTS_DIR/lib.sh"
source "$TESTS_DIR/../internal/env.sh"
source "$TESTS_DIR/../internal/refs/refs.sh"

F=$(new_fixture)
trap 'rm -rf "$F"' EXIT
D="$F/real/cat"

# case <label> <nix-body> <expected paths, newline separated, may be empty>
case_() {
    local label="$1" body="$2" expected="$3" file="$D/case.nix"
    write "$file" "$body"
    local got
    got=$(refs::paths "$file" | sort -u)
    expected=$(printf '%s' "$expected" | sort -u)
    if [[ "$got" == "$expected" ]]; then
        pass "$label"
    else
        fail "$label" "expected [$(tr '\n' ' ' <<< "$expected")] got [$(tr '\n' ' ' <<< "$got")]"
    fi
}

section "path spellings the parser resolves"
case_ "imports sibling"          '{ imports = [ ./x.nix ]; }'                    "$D/x.nix"
case_ "imports parent escape"    '{ imports = [ ../system/w.nix ]; }'            "$F/real/system/w.nix"
case_ "imports directory"        '{ imports = [ ./unit ]; }'                     "$D/unit"
case_ "value import"             'let t = import ../theme.nix; in { }'           "$F/real/theme.nix"
case_ "readFile non-nix"         '{ x = builtins.readFile ../wall.png; }'        "$F/real/wall.png"
case_ "interpolated in string"   '{ x = "${../wall.png}"; }'                     "$F/real/wall.png"
case_ "indented string interp"   "{ x = ''\${../wall.png}''; }"                  "$F/real/wall.png"
case_ "dot path"                 '{ x = ./.; }'                                  "$D"
case_ "absolute literal"         '{ imports = [ /etc/nixos/x.nix ]; }'           "/etc/nixos/x.nix"
case_ "multi-line + comments"    $'{\n  imports = [\n    # ../commented.nix\n    ./a.nix # ../trailing.nix\n  ];\n}' "$D/a.nix"

section "non-path references (must yield nothing)"
case_ "flake input module"       '{ inputs, ... }: { imports = [ inputs.foo.nixosModules.default ]; }' ""
case_ "luxos.modules call"       '{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }'          ""
case_ "path-looking string"      '{ description = "see ../x.nix and (/etc/foo)"; }'                     ""
case_ "update operator //"       '{ x = { a = 1; } // { b = 2; }; }'                                    ""
case_ "division"                 '{ x = 4 / 2; }'                                                       ""
case_ "angle bracket <nixpkgs>"  '{ x = <nixpkgs>; }'                                                   ""

section "dynamic spellings: refs::paths sees only the static base (the hole)"
case_ "path + string suffix"     '{ imports = [ (./. + "/../escape.nix") ]; }'   "$D"
case_ "interpolated path"        'let n = "x"; in { imports = [ ./${n}.nix ]; }' "$D/"
case_ "root + string"            '{ imports = [ (/. + "home/escape.nix") ]; }'   ""

section "dynamic spellings: refs::dynamic_paths closes the hole"
# dyn <label> <nix-body> <expect: yes|no>
dyn() {
    local file="$D/dyn.nix"
    write "$file" "$2"
    local got
    got=$(refs::dynamic_paths "$file")
    if [[ "$3" == yes && -n "$got" ]] || [[ "$3" == no && -z "$got" ]]; then
        pass "$1${got:+ -> $(tr '\n' ' ' <<< "$got")}"
    else
        fail "$1" "expected dynamic=$3, got [$got]"
    fi
}
dyn "path + string suffix"       '{ imports = [ (./. + "/../escape.nix") ]; }'             yes
dyn "interpolated path"          'let n = "x"; in { imports = [ ./${n}.nix ]; }'           yes
dyn "interpolated escape"        '{ imports = [ ./sub/${"../../../escape"}.nix ]; }'       yes
dyn "root + string"              '{ imports = [ (/. + "home/escape.nix") ]; }'             yes
dyn "static path (not dynamic)"  '{ imports = [ ./a.nix ../b.nix ]; }'                     no
dyn "number addition"            '{ x = 1 + 2; }'                                          no
dyn "string concat"              '{ x = "a" + "b"; }'                                      no
dyn "list concat ++"             '{ x = [ ./a.nix ] ++ [ ./b.nix ]; }'                     no
dyn "string containing ./x + "   '{ x = "(./x + y)"; }'                                    no

section "symlinks: lexical vs physical resolution"
ln -s "$F/real" "$F/link"
write "$F/real/cat/sym.nix" '{ imports = [ ../system/w.nix ]; }'
got=$(refs::paths "$F/link/cat/sym.nix")
echo "  parsed via $F/link/cat/sym.nix -> $got"
if [[ "$got" == "$F/link/system/w.nix" ]]; then
    pass "resolves lexically against the given path (symlink kept)"
elif [[ "$got" == "$F/real/system/w.nix" ]]; then
    pass "resolves physically (symlink dereferenced)"
else
    fail "unexpected symlink resolution" "$got"
fi

# Symlinked directory mid-path, reached from inside it: does ".." go back to
# the link's parent (lexical) or the target's parent (physical)?
mkdir -p "$F/fw/mods" "$F/cfg/modules"
ln -s "$F/fw/mods" "$F/cfg/modules/system"
write "$F/fw/mods/wayland.nix" '{ imports = [ ../desktop.nix ]; }'
got=$(refs::paths "$F/cfg/modules/system/wayland.nix")
echo "  parsed via cfg/modules/system (link) -> $got"
case "$got" in
    "$F/cfg/modules/desktop.nix") pass ".. through dir symlink is lexical (stays in cfg/modules)" ;;
    "$F/fw/desktop.nix")          pass ".. through dir symlink is physical (lands in fw/)" ;;
    *)                            fail "unexpected .. through dir symlink" "$got" ;;
esac

# A relative file argument would resolve against the physical cwd rather
# than any symlink in its own path (the exact footgun §3 warns about:
# "always pass the parser an absolute file path"). refs::paths closes it by
# refusing outright instead of silently resolving against the wrong tree.
if err=$(cd "$F/link/cat" && refs::paths sym.nix 2>&1); then
    fail "relative file arg should be refused" "got: $err"
else
    pass "relative file arg refused -> $err"
fi

summary
