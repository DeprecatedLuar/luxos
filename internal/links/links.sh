#!/usr/bin/env bash
set -euo pipefail

# Symlink management: /etc/nixos healing, and the per-machine union folders
# (users/services/modules) that give each machine access to every other
# machine's and the framework's shareable files.
# Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR) first.
# Every function here aborts the process (exit 1) on error rather than
# returning non-zero — callers do not check return codes.

LINKS_ETC_NIXOS="/etc/nixos"
LINKS_ETC_NIXOS_ENV_MODE="600"

# Reserved union entry name for the framework's own modules link
# (<kind>/system -> $FRAMEWORK_DIR/modules). No machine may author it.
LINKS_FRAMEWORK_MODULE_NAME="system"

#──[/etc/nixos healing]────────────────────────────────────────────────────────

# Ensure /etc/nixos is a real directory holding only hardware-configuration.nix
# and env — configuration.nix now lives under $STAGING_DIR (see
# internal/staging/staging.sh), not here. Idempotent; self-heals a stale
# symlink from the old single-file scheme, and removes the old
# configuration.nix symlink if one is still present from before the flake
# migration.
links::ensure_etc_nixos() {
    if [[ -L "$LINKS_ETC_NIXOS" ]]; then
        echo "Converting $LINKS_ETC_NIXOS from symlink to real directory..."
        sudo rm -f "$LINKS_ETC_NIXOS"
    fi

    if [[ ! -d "$LINKS_ETC_NIXOS" ]]; then
        echo "Creating $LINKS_ETC_NIXOS directory..."
        sudo mkdir -p "$LINKS_ETC_NIXOS"
    fi

    if [[ -L "$LINKS_ETC_NIXOS/configuration.nix" ]]; then
        echo "Removing stale $LINKS_ETC_NIXOS/configuration.nix symlink..."
        sudo rm -f "$LINKS_ETC_NIXOS/configuration.nix"
    fi

    # For service environmentFiles that expect it to exist even when empty
    if [[ ! -f "$LINKS_ETC_NIXOS/env" ]]; then
        echo "Creating $LINKS_ETC_NIXOS/env..."
        sudo touch "$LINKS_ETC_NIXOS/env"
        sudo chmod "$LINKS_ETC_NIXOS_ENV_MODE" "$LINKS_ETC_NIXOS/env"
    fi
}

#──[Union folders]─────────────────────────────────────────────────────────────

# Record that `name` (sourced from `src`) claims a slot under `context`,
# erroring if a different source already claimed it. Shared by every fold
# below so a name must be defined exactly once, regardless of which source
# placed it.
_links_claim_name() {
    local -n _seen="$1"
    local name="$2" src="$3" context="$4"

    if [[ -n "${_seen[$name]:-}" ]]; then
        echo "Error: duplicate '$name' for $context" >&2
        echo "  ${_seen[$name]}" >&2
        echo "  $src" >&2
        echo "  A name must be defined once; rename one of them." >&2
        exit 1
    fi
    _seen[$name]="$src"
}

# Remove every generated entry from a previous build_union run. Reads the
# folder's own .gitignore for the exact list it wrote last time; falls back
# to deleting every symlink at any depth when no .gitignore exists yet (first
# run). Never touches a real file: if a listed name is no longer a symlink,
# something clobbered it — error loudly rather than silently deleting or
# skipping it.
_links_clear_generated() {
    local dir="$1"
    local gitignore="$dir/.gitignore"

    if [[ -f "$gitignore" ]]; then
        local entry path
        while IFS= read -r entry; do
            [[ -z "$entry" || "$entry" == "default.nix" || "$entry" == ".gitignore" ]] && continue
            path="$dir/${entry%/}"
            if [[ -e "$path" && ! -L "$path" ]]; then
                echo "Error: real file where a generated union entry was expected: $path" >&2
                echo "  Union folders hold only symlinks (plus the generated default.nix)." >&2
                echo "  Move it into a machine's own authored tree." >&2
                exit 1
            fi
            [[ -L "$path" ]] && rm -f "$path"
        done < "$gitignore"
    else
        find "$dir" -type l -delete
    fi
}

# Stow-style fold: for every entry in peer_dir, link the deepest node that
# does not already exist locally under local_dir.
#   - Nothing local with that name           -> fold: one symlink for the
#                                                whole peer subtree.
#   - Local already has a real dir, and the
#     peer entry is also a dir               -> unfold: recurse, link children.
#   - Local already has a real file/dir and
#     the peer entry is the other type       -> can't fold a file into a
#                                                category or vice versa; error.
#   - Local already has a real file, peer
#     has a same-named real file             -> local authorship wins, skip.
# rel_prefix accumulates the path (relative to local_dir's root) used both
# for collision messages and for the generated-entries list the caller writes
# to .gitignore. `seen` and `generated` are caller-owned nameref arrays.
_links_fold_tree() {
    local local_dir="$1" peer_dir="$2" rel_prefix="$3" source_desc="$4"
    local -n _ft_seen="$5"
    local -n _ft_generated="$6"

    local -a entries=()
    mapfile -d '' -t entries < <(LC_ALL=C find "$peer_dir" -mindepth 1 -maxdepth 1 -print0 | LC_ALL=C sort -z)

    local entry name local_entry rel
    for entry in "${entries[@]}"; do
        name=$(basename "$entry")
        local_entry="$local_dir/$name"
        rel="${rel_prefix}${name}"

        if [[ -e "$local_entry" && ! -L "$local_entry" ]]; then
            if [[ -d "$local_entry" && -d "$entry" ]]; then
                _links_fold_tree "$local_entry" "$entry" "$rel/" "$source_desc" _ft_seen _ft_generated
            elif [[ -d "$local_entry" || -d "$entry" ]]; then
                echo "Error: '$rel' is a directory on one side and a file on the other." >&2
                echo "  local: $local_entry" >&2
                echo "  peer:  $entry" >&2
                echo "  Can't fold a file into a category, or a category into a unit." >&2
                exit 1
            fi
            # Both real files with the same name: local authorship wins.
            continue
        fi

        _links_claim_name _ft_seen "$rel" "$source_desc: $entry" "$local_dir"
        ln -sr "$entry" "$local_entry"
        _ft_generated+=("$rel")
    done
}

# Write the folder's generated .gitignore: exactly the entries just
# generated, plus default.nix and this file itself — otherwise .gitignore is
# tracked and its contents (which change whenever a peer machine gains a
# file) show up as a diff this machine's user never authored. Sorted
# LC_ALL=C so a rerun with no change is byte-identical.
_links_write_gitignore() {
    local dir="$1"
    local -n _wg_names="$2"

    local -a all=(default.nix .gitignore "${_wg_names[@]}")
    printf '%s\n' "${all[@]}" | LC_ALL=C sort > "$dir/.gitignore"
}

# Build the union for one kind (users/modules/services) inside one machine:
# its own authored files stay untouched as real files; everything else it
# can reach — the framework's modules (modules kind only), and every given
# peer's matching folder — is folded in as symlinks, stow-style (see
# _links_fold_tree). Rebuilt from scratch on every self-heal. Pure placement:
# the caller supplies which kind and which peers, in the order it wants them
# tried (self_heal::run owns discovering both).
links::build_union() {
    local machine_dir="$1" kind="$2"
    shift 2
    local -a peer_dirs=("$@")
    local target_dir="$machine_dir/$kind"

    mkdir -p "$target_dir"
    _links_clear_generated "$target_dir"

    local -A seen=()
    local -a generated=()

    if [[ "$kind" == "modules" ]]; then
        local system_link="$target_dir/$LINKS_FRAMEWORK_MODULE_NAME"
        if [[ -e "$system_link" && ! -L "$system_link" ]]; then
            echo "Error: '$system_link' is a real file/dir." >&2
            echo "  '$LINKS_FRAMEWORK_MODULE_NAME' is reserved for the framework modules link; a machine may not author it." >&2
            exit 1
        fi
        if [[ ! -d "$FRAMEWORK_DIR/modules" ]]; then
            echo "Error: framework modules directory not found: $FRAMEWORK_DIR/modules" >&2
            exit 1
        fi
        _links_claim_name seen "$LINKS_FRAMEWORK_MODULE_NAME" "framework: $FRAMEWORK_DIR/modules" "$target_dir"
        ln -s "$FRAMEWORK_DIR/modules" "$system_link"
        generated+=("$LINKS_FRAMEWORK_MODULE_NAME")
    fi

    local peer_dir peer_kind_dir peer_name
    for peer_dir in ${peer_dirs[@]+"${peer_dirs[@]}"}; do
        peer_kind_dir="$peer_dir/$kind"
        [[ -d "$peer_kind_dir" ]] || continue
        peer_name=$(basename "$peer_dir")
        _links_fold_tree "$target_dir" "$peer_kind_dir" "" "machine $peer_name" seen generated
    done

    _links_write_gitignore "$target_dir" generated
}
