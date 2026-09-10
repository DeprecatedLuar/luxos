#!/usr/bin/env bash
set -euo pipefail

# Generates configuration.nix from a machine's machine.toml and flake.nix from
# CONFIG_DIR/channels.toml, and exposes discovery of the users/modules/services
# folders a machine's configuration.nix imports from (see
# implementation-plan.md §3 for the name-resolution rule these folders share).
# Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR) first.
#
# Every function here aborts the process (exit 1) on error rather than
# returning non-zero — callers do not check return codes. The one exception is
# _configgen_find_unit, whose "not found" is a real answer and returns 1.

# Hand-edited declaration of the flake's inputs, at CONFIG_DIR root.
CONFIGGEN_CHANNELS_FILE="channels.toml"

# The name-array kinds a machine.toml declares, and therefore the union folders
# a machine carries. Declared once here — configgen owns machine.toml's schema —
# and passed to every other package by the orchestrator.
CONFIGGEN_KINDS=(users modules services)

# The platform every generated flake builds for.
CONFIGGEN_NIX_SYSTEM="x86_64-linux"

# configuration.nix's fixed imports, relative to $STAGING_DIR where it is
# written. Each <kind> resolves to that folder's own generated default.nix;
# no name resolution happens in configuration.nix itself.
CONFIGGEN_MACHINE_IMPORTS=(
    "./framework/system.nix"
    "./config/users"
    "./config/modules"
    "./config/services"
    "./config/local"
)

# system.nix references inputs.nixpkgs (nix.registry, nix.nixPath), so the
# base channel is always emitted under this input name — its declared name
# only ever appears in the overlay.
CONFIGGEN_BASE_INPUT_NAME="nixpkgs"

# Names the generator emits itself; a channel/flake may not claim them.
CONFIGGEN_RESERVED_NAMES=("self" "$CONFIGGEN_BASE_INPUT_NAME")

# A channel/flake name becomes a Nix attribute and a flake input name.
CONFIGGEN_NAME_REGEX='^[a-zA-Z_][a-zA-Z0-9_-]*$'

# Generated files at CONFIG_DIR root, ignored by the generated root
# .gitignore. flake.nix and configuration.nix are written straight into
# $STAGING_DIR (see configgen::generate/generate_flake) and never land in
# CONFIG_DIR at all, so local/default.nix is the only entry left here — the
# union folders' own default.nix/.gitignore are declared by links.sh instead.
CONFIGGEN_GITIGNORE_FILE=".gitignore"
CONFIGGEN_IGNORED_PATHS=(
    "/*/local/default.nix"
)

#──[Discovery]──────────────────────────────────────────────────────────────

# The one walk every discovery/resolution function below is built from.
# Applies rule 1 (implementation-plan.md §3): a directory with its own
# default.nix is a unit — its own name is the name, never look inside it. A
# directory without one is a transparent category — descend through it. The
# generated aggregator (<kind>/default.nix) is never itself a declarable name.
# Emits "name<TAB>relpath" for every unit found, relpath relative to $dir.
_configgen_walk_units() {
    local dir="$1" prefix="${2:-}"
    local entry base
    for entry in "$dir"/*; do
        [[ -e "$entry" ]] || continue
        base=$(basename "$entry")
        if [[ -d "$entry" ]]; then
            if [[ -f "$entry/default.nix" ]]; then
                printf '%s\t%s\n' "$base" "${prefix}${base}/default.nix"
            else
                _configgen_walk_units "$entry" "${prefix}${base}/"
            fi
        elif [[ "$entry" == *.nix && "$base" != "default.nix" ]]; then
            printf '%s\t%s\n' "${base%.nix}" "${prefix}${base}"
        fi
    done
}

# List declarable names (e.g. "gaming hyprland") under
# <machine_dir>/<kind> (kind: users, modules, or services).
configgen::discover() {
    local machine_dir="$1" kind="$2"
    local dir="$machine_dir/$kind"
    [[ -d "$dir" ]] || return 0
    _configgen_walk_units "$dir" | cut -f1 | LC_ALL=C sort
}

# Error loudly if any name under <machine_dir>/<kind> is claimed by more than
# one path — e.g. a peer machine and the framework both shipping a
# same-named module, folded together by the union with no other check
# catching it (implementation-plan.md §3: "a name must be defined once").
configgen::validate_unique_names() {
    local machine_dir="$1" kind="$2"
    local dir="$machine_dir/$kind"
    [[ -d "$dir" ]] || return 0

    local dup name
    dup=$(_configgen_walk_units "$dir" | LC_ALL=C sort | cut -f1 | uniq -d)
    [[ -z "$dup" ]] && return 0

    while IFS= read -r name; do
        [[ -n "$name" ]] || continue
        echo "Error: '$name' is claimed by more than one path under $dir:" >&2
        _configgen_walk_units "$dir" | awk -F'\t' -v n="$name" '$1==n {print "  " $2}' >&2
        exit 1
    done <<< "$dup"
}

# List machine names — one per directory at CONFIG_DIR root containing a
# machine.toml. This is the authoritative machine list; flake.nix's
# nixosConfigurations is generated from it, so adding a machine is only ever
# adding a directory. Machines now share the root with flake.nix, .git and
# anything else the user keeps there, so presence of machine.toml (not just
# being a directory) is what qualifies an entry.
configgen::discover_machines() {
    local entry
    for entry in "$CONFIG_DIR"/*/; do
        [[ -f "${entry}machine.toml" ]] || continue
        basename "$entry"
    done | LC_ALL=C sort
}

# Resolve a machine name to its directory. Hard error naming the path tried
# if it doesn't hold a machine.toml — the caller never has to check itself.
configgen::resolve_machine() {
    local name="$1"
    local dir="$CONFIG_DIR/$name"
    if [[ ! -f "$dir/machine.toml" ]]; then
        echo "Error: no machine.toml under $dir" >&2
        exit 1
    fi
    echo "$dir"
}

# Resolve <name> to its path (relative to <machine_dir>/<kind>) by applying
# rule 1 via _configgen_walk_units. Echoes the path and returns 0 on success;
# returns 1 with no output if <name> isn't declared anywhere under the
# directory — this is the one function here where "not found" is a real
# answer, so it returns rather than exits.
_configgen_find_unit() {
    local dir="$1" name="$2"
    local path
    path=$(_configgen_walk_units "$dir" | awk -F'\t' -v n="$name" '$1==n { print $2; exit }')
    [[ -n "$path" ]] || return 1
    echo "$path"
}

#──[TOML parsing]───────────────────────────────────────────────────────────
# Private helpers — only configgen.sh itself calls these.

# Parse TOML string value (key = "value")
parse_string() {
    local file=$1
    local key=$2
    grep -E "^${key}[[:space:]]*=" "$file" | sed 's/.*= *"\(.*\)".*/\1/' | head -1
}

# Parse TOML array (key = ["a", "b", "c"])
parse_array() {
    local file=$1
    local key=$2
    grep -E "^${key}[[:space:]]*=" "$file" | sed 's/.*= *\[\(.*\)\].*/\1/' | tr ',' '\n' | sed 's/^[" \t]*//; s/[" \t]*$//' | grep -v '^$' || true
}

# Public wrapper over parse_array for a machine.toml's users/modules/services
# arrays — the one thing outside configgen.sh that needs those declared
# names. Callers must not call parse_array directly; it stays private.
configgen::declared_names() {
    local machine_toml="$1" kind="$2"
    parse_array "$machine_toml" "$kind"
}

# Emit "key<TAB>value" for every key belonging to [section] in a TOML file.
# An empty section name selects top-level keys — the ones before the first
# header. Blank lines and comments are skipped, inline comments and the
# surrounding quotes of a string value are stripped.
#
# Deliberately separate from parse_string/parse_array above: those are
# column-0-anchored greps with no concept of a header, so they can't see an
# indented key and can't tell two sections apart. They stay machine.toml's.
_configgen_parse_section() {
    local file="$1"
    local want="$2"
    awk -v want="$want" -v file="$file" '
        { sub(/\r$/, "") }
        /^[[:space:]]*#/ { next }
        /^[[:space:]]*$/ { next }
        /^[[:space:]]*\[/ {
            section = $0
            sub(/^[[:space:]]*\[[[:space:]]*/, "", section)
            sub(/[[:space:]]*\].*$/, "", section)
            current = section
            next
        }
        {
            eq = index($0, "=")
            if (eq == 0) {
                printf "Error: %s:%d is neither a header nor a key = value\n", file, NR > "/dev/stderr"
                exit 1
            }
            key = substr($0, 1, eq - 1)
            val = substr($0, eq + 1)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
            sub(/^[[:space:]]+/, "", val)
            if (substr(val, 1, 1) == "\"") {
                close_at = index(substr(val, 2), "\"")
                if (close_at == 0) {
                    printf "Error: %s:%d has an unterminated quoted value\n", file, NR > "/dev/stderr"
                    exit 1
                }
                val = substr(val, 2, close_at - 1)
            } else {
                sub(/#.*$/, "", val)
                sub(/[[:space:]]+$/, "", val)
            }
            if (current == want) print key "\t" val
        }
    ' "$file"
}

# Validate TOML file structure. "Required" and "allowed" are kept separate on
# purpose: a missing required key is a hard error, but an unknown key only
# warns — it might be a newly added optional key the parser doesn't know
# about yet, and hard-erroring on it would actively block schema changes.
validate_toml() {
    local file=$1
    local required_keys=("hostName" "timeZone" "locale" "stateVersion" "users" "modules" "services")
    # Extend here as optional keys are added to machine.toml's schema.
    local allowed_keys=("${required_keys[@]}")
    local errors=()

    # Check for required keys, anchored so e.g. "hostName" doesn't also match
    # a hypothetical "hostNameExtra".
    local key
    for key in "${required_keys[@]}"; do
        if ! grep -qE "^${key}[[:space:]]*=" "$file"; then
            errors+=("Missing required key: $key")
        fi
    done

    # Warn on unexpected keys (non-comment, non-empty lines). Column-0
    # anchored like the rest of machine.toml's parsing, so an indented or
    # continuation line is skipped rather than misread as a new key.
    local line
    while IFS= read -r line; do
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ "$line" =~ ^[[:space:]]*$ ]] && continue
        [[ "$line" =~ ^([a-zA-Z_][a-zA-Z0-9_]*)[[:space:]]*= ]] || continue

        key="${BASH_REMATCH[1]}"
        if [[ ! " ${allowed_keys[*]} " =~ " $key " ]]; then
            echo "Warning: unknown key '$key' in $file" >&2
        fi
    done < "$file"

    # Report errors
    if [[ ${#errors[@]} -gt 0 ]]; then
        echo "Error: Invalid TOML format in $file"
        printf '  - %s\n' "${errors[@]}"
        exit 1
    fi
}

# Append one machine.toml array field as a comment block of its discovered
# options followed by the (empty) array itself. Shared by every kind in
# _configgen_generate_template below instead of three copy-pasted blocks.
_configgen_template_field() {
    local machine_dir="$1" kind="$2" output="$3"
    local -a available
    mapfile -t available < <(configgen::discover "$machine_dir" "$kind")

    echo "# ${kind^} to import from $machine_dir/$kind/" >> "$output"
    if [[ ${#available[@]} -gt 0 ]]; then
        echo "# Available:" >> "$output"
        local name
        for name in "${available[@]}"; do
            echo "#   - $name" >> "$output"
        done
    fi
    echo "$kind = []" >> "$output"
}

# Generate template with auto-discovered options
_configgen_generate_template() {
    local machine_dir="$1" output="$2"

    cat > "$output" << 'EOF'
# NixOS Machine Configuration
# Auto-generated template - edit as needed

EOF

    # Users first — most important: first entry becomes the main user.
    echo "# First user is the main user (auto-login, home directory for configs)" >> "$output"
    _configgen_template_field "$machine_dir" users "$output"

    cat >> "$output" << 'EOF'
hostName = "hostname"
timeZone = "America/Sao_Paulo"
locale = "en_US.UTF-8"
stateVersion = "25.05"

EOF

    local kind
    for kind in "${CONFIGGEN_KINDS[@]}"; do
        [[ "$kind" == "users" ]] && continue
        _configgen_template_field "$machine_dir" "$kind" "$output"
        echo "" >> "$output"
    done
}

#──[Generation]─────────────────────────────────────────────────────────────

# Shared "Auto-generated - DO NOT EDIT" banner for every generated file.
# $2 names the file/action to re-run in the third line's instruction.
_configgen_header() {
    local source_desc="$1" instruction="$2"
    cat << EOF
# Auto-generated from $source_desc - DO NOT EDIT
# $instruction
EOF
}

# Generate configuration.nix from a machine directory's machine.toml, to
# $output_file. Written by the caller into $STAGING_DIR — the imports below
# are relative to there (./framework, ./config), not to $machine_dir, so this
# file cannot be evaluated anywhere else.
configgen::generate() {
    local machine_dir="${1:-.}" output_file="$2"
    local machine_toml="$machine_dir/machine.toml"

    # Check if machine.toml exists, create from template if not
    if [[ ! -f "$machine_toml" ]]; then
        echo "Creating $machine_toml from template..."
        _configgen_generate_template "$machine_dir" "$machine_toml"
        echo "✓ Created template with auto-discovered modules and users"
        exit 1
    fi

    # Validate TOML structure
    validate_toml "$machine_toml"

    # Parse variables
    local host_name time_zone locale state_version main_user
    host_name=$(parse_string "$machine_toml" hostName)
    time_zone=$(parse_string "$machine_toml" timeZone)
    locale=$(parse_string "$machine_toml" locale)
    state_version=$(parse_string "$machine_toml" stateVersion)

    # Derive main user from first user in array
    main_user=$(parse_array "$machine_toml" users | head -1)

    # Validate required fields
    if [[ -z "$main_user" ]]; then
        echo "Error: users array must have at least one user in $machine_toml"
        exit 1
    fi

    if [[ -z "$host_name" ]]; then
        echo "Error: hostName is required in $machine_toml"
        exit 1
    fi

    if [[ -z "$state_version" ]]; then
        echo "Error: stateVersion is required in $machine_toml"
        exit 1
    fi

    # No name resolution here: users/modules/services are resolved into their
    # own <kind>/default.nix by configgen::generate_folder_default (called by
    # self-heal, from the same machine.toml arrays), so the imports list
    # below is fixed — every machine imports the same things, relative to
    # $STAGING_DIR where this file is written.
    local imports_block
    imports_block=$(printf '    %s\n' "${CONFIGGEN_MACHINE_IMPORTS[@]}")

    mkdir -p "$(dirname "$output_file")"
    { _configgen_header "machine.toml" \
        "Edit machine.toml instead and run: sudo nixos-rebuild switch"
      echo "# Written by self-heal into \$STAGING_DIR — its imports below only resolve there."
      cat << EOF

{ ... }:

let
  mainUser = "$main_user";
  hostName = "$host_name";
in
{
  time.timeZone = "$time_zone";
  i18n.defaultLocale = "$locale";
  system.stateVersion = "$state_version";

  imports = [
$imports_block
  ];

  networking.hostName = hostName;
  _module.args = { inherit mainUser hostName; };
}
EOF
    } > "$output_file"

    echo "✓ Generated: $output_file"
}

# Generate <machine_dir>/<kind>/default.nix, importing exactly [names...],
# each resolved per rule 1 (see _configgen_find_unit). No names means no
# imports — a machine.toml array declaring nothing gets an empty default.nix,
# never a folder-wide fallback.
#
# For the undeclared, discover-what's-there case (local/), see
# configgen::generate_default — the mode is chosen by the caller, never
# inferred from how many names happened to be passed.
#
# This is the one function that owns the outer default.nix in every kind
# folder — the tool's own generated aggregator, distinct from a unit's own
# hand-written default.nix one level down (e.g. users/luar/default.nix).
configgen::generate_folder_default() {
    local machine_dir="$1" kind="$2"
    shift 2
    local names=("$@")
    local dir="$machine_dir/$kind"
    local output_file="$dir/default.nix"

    local imports=()
    local name path
    for name in ${names[@]+"${names[@]}"}; do
        [[ -n "$name" ]] || continue
        path=$(_configgen_find_unit "$dir" "$name") || {
            echo "Error: $kind '$name' not found under $dir" >&2
            exit 1
        }
        imports+=("    ./$path")
    done

    mkdir -p "$dir"

    local imports_block=""
    [[ ${#imports[@]} -gt 0 ]] && imports_block=$'\n'$(printf '%s\n' "${imports[@]}")$'\n  '

    { _configgen_header "machine.toml" \
        "Generated by self-heal from machine.toml (or, for local/, from whatever is present in this folder). Edit machine.toml, or drop/remove files here, and run: sudo nixos-rebuild switch"
      cat << EOF

{ ... }:

{
  imports = [${imports_block}];
}
EOF
    } > "$output_file"

    echo "✓ Generated: $output_file"
}

# local/ is machine-private, always-on config with no array in machine.toml:
# whatever is present gets imported. Discovery happens here, explicitly, so
# generate_folder_default never has to guess the mode from its argument count.
configgen::generate_default() {
    local machine_dir="${1:-.}"
    local dir="$machine_dir/local"

    local names=()
    if [[ -d "$dir" ]]; then
        local name
        while IFS= read -r name; do
            [[ -n "$name" ]] && names+=("$name")
        done < <(configgen::discover "$machine_dir" local)
    fi

    configgen::generate_folder_default "$machine_dir" local ${names[@]+"${names[@]}"}
}

#──[Flake generation]───────────────────────────────────────────────────────

# Reject a channel/flake name that can't be a flake input or an overlay attr.
_configgen_validate_name() {
    local name="$1"
    local file="$2"
    local reserved

    if [[ ! "$name" =~ $CONFIGGEN_NAME_REGEX ]]; then
        echo "Error: '$name' in $file is not a valid Nix identifier" >&2
        echo "  Names must match $CONFIGGEN_NAME_REGEX" >&2
        exit 1
    fi

    for reserved in "${CONFIGGEN_RESERVED_NAMES[@]}"; do
        if [[ "$name" == "$reserved" ]]; then
            echo "Error: '$name' in $file is reserved — the generator emits it itself" >&2
            exit 1
        fi
    done
}

# Warn when flake.lock's inputs no longer match the ones just generated.
# Renaming a channel orphans its lock entry and silently re-resolves that
# input to whatever HEAD is today, which is otherwise invisible.
_configgen_warn_lock_drift() {
    local lock="$CONFIG_DIR/flake.lock"
    [[ -f "$lock" ]] || return 0

    if ! command -v jq >/dev/null 2>&1; then
        echo "Error: jq not found — cannot check $lock for drift" >&2
        echo "  jq is a framework dependency (see system.nix); reinstall it or use --bypass." >&2
        exit 1
    fi

    local locked generated name
    locked=$(jq -r '.nodes.root.inputs | keys[]' "$lock" | LC_ALL=C sort)
    generated=$(printf '%s\n' "$@" | LC_ALL=C sort)
    [[ "$locked" == "$generated" ]] && return 0

    echo "Warning: $lock no longer matches $CONFIGGEN_CHANNELS_FILE" >&2
    while IFS= read -r name; do
        [[ -n "$name" ]] && echo "  orphaned in flake.lock: $name" >&2
    done < <(comm -23 <(echo "$locked") <(echo "$generated"))
    while IFS= read -r name; do
        [[ -n "$name" ]] && echo "  unlocked, will resolve to HEAD: $name" >&2
    done < <(comm -13 <(echo "$locked") <(echo "$generated"))
    echo "  Run 'nixos-rebuild --update-lock' to re-lock deliberately." >&2
}

# Generate flake.nix at $output_file from CONFIG_DIR/channels.toml, for the
# one machine being staged. Every declared nixpkgs channel becomes both a
# flake input and an overlay attribute, so a package reference always states
# its origin (stable.rofi, unstable.hyprland). Everything is emitted in
# LC_ALL=C sorted order, so a rerun with no source change is byte-identical.
#
# Written to $STAGING_DIR, never to CONFIG_DIR: its own ./configuration.nix
# import only exists there. One nixosConfigurations entry, not one per
# machine, for the same reason — staging::materialize copies the active
# machine alone (as ./config, unions dereferenced), so ./configuration.nix
# at the flake root describes that machine and nothing else. Enumerating
# every machine here would point every attribute at the same file —
# nixosConfigurations.nuremberg would build paraloid.
configgen::generate_flake() {
    local host_name="$1" output_file="$2"
    local channels_toml="$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE"

    if [[ -z "$host_name" ]]; then
        echo "Error: configgen::generate_flake requires the machine name" >&2
        exit 1
    fi

    if [[ ! -f "$channels_toml" ]]; then
        echo "Error: $channels_toml not found — run 'setup' to scaffold it" >&2
        exit 1
    fi

    local toplevel channels_raw flakes_raw
    toplevel=$(_configgen_parse_section "$channels_toml" "")
    channels_raw=$(_configgen_parse_section "$channels_toml" channels | LC_ALL=C sort)
    flakes_raw=$(_configgen_parse_section "$channels_toml" flakes | LC_ALL=C sort)

    local key value
    local base=""
    while IFS=$'\t' read -r key value; do
        case "$key" in
            "") ;;
            base) base="$value" ;;
            *)
                echo "Error: unknown top-level key '$key' in $channels_toml" >&2
                exit 1
                ;;
        esac
    done <<< "$toplevel"

    if [[ -z "$base" ]]; then
        echo "Error: 'base' is required in $channels_toml" >&2
        echo "  It names the channel that builds the system (provides lib)." >&2
        exit 1
    fi

    local channel_names=() channel_urls=()
    while IFS=$'\t' read -r key value; do
        [[ -n "$key" ]] || continue
        _configgen_validate_name "$key" "$channels_toml"
        channel_names+=("$key")
        channel_urls+=("$value")
    done <<< "$channels_raw"

    local flake_names=() flake_urls=()
    while IFS=$'\t' read -r key value; do
        [[ -n "$key" ]] || continue
        _configgen_validate_name "$key" "$channels_toml"
        flake_names+=("$key")
        flake_urls+=("$value")
    done <<< "$flakes_raw"

    if [[ ${#channel_names[@]} -eq 0 ]]; then
        echo "Error: $channels_toml declares no [channels] entry" >&2
        exit 1
    fi

    local duplicate
    duplicate=$(printf '%s\n' "${channel_names[@]}" ${flake_names[@]+"${flake_names[@]}"} \
        | LC_ALL=C sort | uniq -d)
    if [[ -n "$duplicate" ]]; then
        echo "Error: name declared more than once in $channels_toml:" >&2
        printf '  - %s\n' $duplicate >&2
        exit 1
    fi

    if ! printf '%s\n' "${channel_names[@]}" | grep -qxF "$base"; then
        echo "Error: base = \"$base\" is not a declared [channels] entry in $channels_toml" >&2
        exit 1
    fi

    # Base channel first, then the rest in sorted order — the base is the one
    # asymmetric entry (it owns the nixpkgs input name and aliases prev).
    local inputs_block="" overlay_block="" outputs_args="self, $CONFIGGEN_BASE_INPUT_NAME"
    local lock_inputs=("$CONFIGGEN_BASE_INPUT_NAME")
    local i name url
    for i in "${!channel_names[@]}"; do
        [[ "${channel_names[$i]}" == "$base" ]] || continue
        inputs_block+="    $CONFIGGEN_BASE_INPUT_NAME.url = \"${channel_urls[$i]}\";  # base channel: $base"$'\n'
        overlay_block+="      $base = prev;"$'\n'
    done
    for i in "${!channel_names[@]}"; do
        name="${channel_names[$i]}"
        url="${channel_urls[$i]}"
        [[ "$name" != "$base" ]] || continue
        inputs_block+="    $name.url = \"$url\";"$'\n'
        overlay_block+="      $name = import $name { inherit (prev.stdenv.hostPlatform) system; inherit (prev) config; };"$'\n'
        outputs_args+=", $name"
        lock_inputs+=("$name")
    done
    for i in ${flake_names[@]+"${!flake_names[@]}"}; do
        inputs_block+="    ${flake_names[$i]}.url = \"${flake_urls[$i]}\";"$'\n'
        lock_inputs+=("${flake_names[$i]}")
    done

    inputs_block="${inputs_block%$'\n'}"
    overlay_block="${overlay_block%$'\n'}"

    mkdir -p "$(dirname "$output_file")"
    { _configgen_header "$CONFIGGEN_CHANNELS_FILE" \
        "Edit $CONFIGGEN_CHANNELS_FILE and run: sudo nixos-rebuild switch"
      cat << EOF

{
  description = "luxos machine configurations";

  inputs = {
$inputs_block
  };

  outputs = { $outputs_args, ... }@inputs:
  let
    system = "$CONFIGGEN_NIX_SYSTEM";

    # Exposes every declared channel as pkgs.<channel>.<package>, so a package
    # reference always states which channel it came from. The base channel
    # aliases prev instead of re-importing itself (pkgs already is that
    # channel); the others inherit config so allowUnfree and friends carry
    # over rather than being restated here.
    channelOverlay = final: prev: {
$overlay_block
    };

    # Machine modules reach the framework's through the union
    # (modules/system -> \$FRAMEWORK_DIR/modules, dereferenced by staging),
    # so no specialArg carries a framework path across the repo boundary.
    mkHost = hostName: $CONFIGGEN_BASE_INPUT_NAME.lib.nixosSystem {
      inherit system;
      specialArgs = { inherit inputs hostName; };
      modules = [
        ./configuration.nix
        { nixpkgs.overlays = [ channelOverlay ]; }
      ];
    };
  in {
    # The staged flake root holds exactly one machine's configuration.nix, so
    # it exposes exactly one host. Regenerated per machine on every run.
    nixosConfigurations.$host_name = mkHost "$host_name";
  };
}
EOF
    } > "$output_file"

    _configgen_warn_lock_drift "${lock_inputs[@]}"

    echo "✓ Generated: $output_file"
}

# Generate CONFIG_DIR's root .gitignore. The generator is the only thing that
# knows what it generates, so it declares that itself rather than leaving a
# hand-maintained list to drift out of sync with it.
configgen::generate_root_gitignore() {
    local output_file="$CONFIG_DIR/$CONFIGGEN_GITIGNORE_FILE"

    printf '%s\n' "${CONFIGGEN_IGNORED_PATHS[@]}" > "$output_file"

    echo "✓ Generated: $output_file"
}
