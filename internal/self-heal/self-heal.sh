#!/usr/bin/env bash
set -euo pipefail

# Self-healing sequence: ensures the framework modules link, the active
# host's kind mirror, and its root local link exist, regenerates every
# default.nix, materializes staging, generates flake.nix and
# configuration.nix straight into it, then heals /etc/nixos to match.
# Pure sequence — no flag parsing, no hostname resolution, no knowledge of the
# real nixos-rebuild binary. That's rebuild's job. links.sh and configgen.sh
# stay pure black boxes: given a host/kind they place or generate files; they
# don't decide what those are.
# Callers must source internal/env.sh first.

SELF_HEAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SELF_HEAL_DIR/../links/links.sh"
source "$SELF_HEAL_DIR/../configgen/configgen.sh"
source "$SELF_HEAL_DIR/../staging/staging.sh"
source "$SELF_HEAL_DIR/../imports/imports.sh"

self_heal::run() {
    local machine_dir="$1"
    local prune="${2:-false}"
    local machine_toml="$machine_dir/machine.toml"
    local machine_name
    machine_name=$(basename "$machine_dir")

    configgen::generate_root_gitignore

    echo "Ensuring framework modules link..."
    links::ensure_framework_link

    # Scaffold any missing kind entrypoint as an empty { imports = []; }
    # before ensure_mirror runs — otherwise the mirror symlink would dangle
    # and staging's dangling-link check would hard-fail the rebuild.
    local kind
    for kind in $(configgen::discover_kinds); do
        local entrypoint="$machine_dir/$kind/default.nix"
        if [[ ! -f "$entrypoint" ]]; then
            echo "Scaffolding empty $entrypoint..."
            mkdir -p "$(dirname "$entrypoint")"
            printf '{ ... }:\n{\n  imports = [];\n}\n' > "$entrypoint"
        fi
    done

    # users is the one kind still selected from machine.toml (users owns
    # that array; modules/services are each host's own hand-written
    # entrypoint) — regenerate its entrypoint from the declared names.
    echo "Generating users/default.nix from machine.toml..."
    local -a declared_users
    mapfile -t declared_users < <(parse_array "$machine_toml" users)
    configgen::generate_folder_default "$machine_dir" users ${declared_users[@]+"${declared_users[@]}"}

    # machine_dir's own entrypoint (.local/<host>/default.nix) — discovery
    # mode over whatever machine-private files are present, excluding the
    # kind mirrors just ensured above and machine.toml itself.
    echo "Generating $machine_name/default.nix from $machine_dir/..."
    configgen::generate_default "$machine_dir"

    echo "Ensuring $machine_name's kind mirror..."
    links::ensure_mirror "$machine_name"

    # Heal every host's modules import lines now that the active host's
    # mirror link (CONFIG_DIR/modules/default.nix -> this host's real
    # entrypoint) exists, and before materialize copies it into staging.
    echo "Healing modules imports..."
    imports::heal "$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/default.nix" "$prune"

    echo "Ensuring local -> .local/$machine_name link..."
    links::ensure_local_link "$machine_name"

    # Materialize the flake root: the shared kind folders (dereferencing both
    # modules/system and every entrypoint symlink) plus this host's own
    # $LOCAL_DIR tree.
    echo "Materializing $STAGING_DIR for $machine_name..."
    staging::materialize "$machine_name"

    # flake.nix and configuration.nix are written last: their imports
    # (./configuration.nix, ./framework/..., ./config/...) only resolve at
    # $STAGING_DIR, which materialize has just built. Generated unprivileged
    # to a temp file, then installed by staging (the one place that sudo
    # cps into $STAGING_DIR).
    local tmp_flake tmp_config
    tmp_flake=$(mktemp)
    echo "Generating flake.nix from channels.toml..."
    configgen::generate_flake "$machine_name" "$tmp_flake"
    staging::install_generated "$tmp_flake" "flake.nix"
    rm -f "$tmp_flake"

    tmp_config=$(mktemp)
    echo "Generating configuration.nix from machine.toml..."
    configgen::generate "$machine_dir" "$tmp_config"
    staging::install_generated "$tmp_config" "configuration.nix"
    rm -f "$tmp_config"

    links::ensure_etc_nixos
}
