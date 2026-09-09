#!/usr/bin/env bash
set -euo pipefail

# Generates configuration.nix from a machine's machine.toml and flake.nix from
# CONFIG_DIR/channels.toml, and exposes discovery of the pools
# (users/modules/services) configuration.nix imports from.
# Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR) first.

# Hand-edited declaration of the flake's inputs, at CONFIG_DIR root.
CONFIGGEN_CHANNELS_FILE="channels.toml"

# system.nix references inputs.nixpkgs (nix.registry, nix.nixPath), so the
# base channel is always emitted under this input name — its declared name
# only ever appears in the overlay.
CONFIGGEN_BASE_INPUT_NAME="nixpkgs"

# Names the generator emits itself; a channel/flake may not claim them.
CONFIGGEN_RESERVED_NAMES=("self" "$CONFIGGEN_BASE_INPUT_NAME")

# A channel/flake name becomes a Nix attribute and a flake input name.
CONFIGGEN_NAME_REGEX='^[a-zA-Z_][a-zA-Z0-9_-]*$'

#──[Discovery]──────────────────────────────────────────────────────────────

# List available basenames (e.g. "gaming hyprland") from a generated pool,
# searched recursively — modules/system/ and any other organizational
# subfolder are transparent to discovery, only the basename matters.
_configgen_discover_pool() {
    local pool="$CONFIG_DIR/.system/$1"
    if [[ -d "$pool" ]]; then
        find -L "$pool" -name "*.nix" -type f \
            | xargs -r -I{} basename {} .nix \
            | sort
    fi
}

configgen::discover_modules() { _configgen_discover_pool modules; }
configgen::discover_users() { _configgen_discover_pool users; }
configgen::discover_services() { _configgen_discover_pool services; }

# List machine names — one per directory under .system/machines/. This is the
# authoritative machine list; flake.nix's nixosConfigurations is generated
# from it, so adding a machine is only ever adding a directory.
configgen::discover_machines() {
    local machines_dir="$CONFIG_DIR/.system/machines"
    if [[ -d "$machines_dir" ]]; then
        find "$machines_dir" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | LC_ALL=C sort
    fi
}

# Canonicalized $CONFIG_DIR/.system/machines/. A pool match is resolved with
# readlink -f, which — when CONFIG_DIR is itself a symlink (e.g. a dotfile
# manager checkout) — canonicalizes through it too, not just the match's own
# symlink. The prefix stripped off that match has to be canonicalized the
# same way, or the strip is a silent no-op and the full absolute path leaks
# into configuration.nix's imports.
_configgen_machines_root() {
    readlink -f "$CONFIG_DIR/.system/machines"
}

# Resolve a module name: echoes "<origin> <path>". "framework" path is
# relative to $STAGING_DIR/framework/modules/ (mirrored wholesale, always
# flat). "machine" path is relative to $STAGING_DIR/config/machines/ (also
# mirrored wholesale), preserving whatever subfolder it actually lives in —
# staging::materialize copies both trees verbatim, so these paths are valid
# in the source repos too, not just once staged.
configgen::resolve_module() {
    local name="$1"

    local fw_match
    fw_match=$(find "$FRAMEWORK_DIR/modules" -name "${name}.nix" -type f -print -quit 2>/dev/null)
    if [[ -n "$fw_match" ]]; then
        echo "framework $(basename "$fw_match")"
        return
    fi

    local pool="$CONFIG_DIR/.system/modules"
    local match
    match=$(find -L "$pool" -name "${name}.nix" -type f -print -quit)
    if [[ -z "$match" ]]; then
        echo "Error: module '$name' not found under $FRAMEWORK_DIR/modules or $pool" >&2
        exit 1
    fi
    match=$(readlink -f "$match")
    echo "machine ${match/#$(_configgen_machines_root)\//}"
}

# Resolve a user/service name to its path relative to
# $CONFIG_DIR/.system/machines/ (== $STAGING_DIR/config/machines/).
_configgen_resolve_pool_entry() {
    local kind="$1" name="$2"
    local pool="$CONFIG_DIR/.system/$kind"
    local match
    match=$(find -L "$pool" -name "${name}.nix" -type f -print -quit)
    if [[ -z "$match" ]]; then
        echo "Error: $kind '$name' not found under $pool" >&2
        exit 1
    fi
    match=$(readlink -f "$match")
    echo "${match/#$(_configgen_machines_root)\//}"
}

configgen::resolve_user() { _configgen_resolve_pool_entry users "$1"; }
configgen::resolve_service() { _configgen_resolve_pool_entry services "$1"; }

#──[TOML parsing]───────────────────────────────────────────────────────────
# Private helpers — only configgen.sh itself calls these.

# Parse TOML string value (key = "value")
parse_string() {
    local file=$1
    local key=$2
    grep "^$key" "$file" | sed 's/.*= *"\(.*\)".*/\1/' | head -1
}

# Parse TOML array (key = ["a", "b", "c"])
parse_array() {
    local file=$1
    local key=$2
    grep "^$key" "$file" | sed 's/.*= *\[\(.*\)\].*/\1/' | tr ',' '\n' | sed 's/^[" \t]*//; s/[" \t]*$//' | grep -v '^$' || true
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

# Convert array to Nix list format ("a" "b" "c")
to_nix_list() {
    local items=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && items+=("\"$line\"")
    done
    # Handle empty arrays
    if [[ ${#items[@]} -eq 0 ]]; then
        echo ""
    else
        echo "${items[@]}"
    fi
}

# Validate TOML file structure
validate_toml() {
    local file=$1
    local required_keys=("hostName" "timeZone" "locale" "stateVersion" "users" "modules" "services")
    local errors=()

    # Check for required keys
    for key in "${required_keys[@]}"; do
        if ! grep -q "^$key" "$file"; then
            errors+=("Missing required key: $key")
        fi
    done

    # Check for unexpected keys (non-comment, non-empty lines)
    while IFS= read -r line; do
        # Skip comments and empty lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ "$line" =~ ^[[:space:]]*$ ]] && continue

        # Extract key name
        key=$(echo "$line" | sed 's/^\([a-zA-Z]*\).*/\1/')

        # Check if key is valid
        if [[ ! " ${required_keys[@]} " =~ " $key " ]]; then
            errors+=("Unknown key: $key")
        fi
    done < "$file"

    # Report errors
    if [[ ${#errors[@]} -gt 0 ]]; then
        echo "Error: Invalid TOML format in $file"
        printf '  - %s\n' "${errors[@]}"
        exit 1
    fi
}

# Generate template with auto-discovered options
_configgen_generate_template() {
    local output=$1
    local available_modules=($(configgen::discover_modules))
    local available_users=($(configgen::discover_users))
    local available_services=($(configgen::discover_services))
    local pool_dir="$CONFIG_DIR/.system"

    cat > "$output" << 'EOF'
# NixOS Machine Configuration
# Auto-generated template - edit as needed

EOF

    # Add available users as comments (FIRST - most important)
    echo "# Users to import from $pool_dir/users/" >> "$output"
    echo "# First user is the main user (auto-login, home directory for configs)" >> "$output"
    if [[ ${#available_users[@]} -gt 0 ]]; then
        echo "# Available:" >> "$output"
        for user in "${available_users[@]}"; do
            echo "#   - $user" >> "$output"
        done
    fi
    echo "users = []" >> "$output"

    cat >> "$output" << 'EOF'
hostName = "hostname"
timeZone = "America/Sao_Paulo"
locale = "en_US.UTF-8"
stateVersion = "25.05"

EOF

    # Add available modules as comments
    echo "# Modules to import from $pool_dir/modules/" >> "$output"
    if [[ ${#available_modules[@]} -gt 0 ]]; then
        echo "# Available:" >> "$output"
        for module in "${available_modules[@]}"; do
            echo "#   - $module" >> "$output"
        done
    fi
    echo "modules = []" >> "$output"

    cat >> "$output" << 'EOF'

EOF
    echo "# Services to import from $pool_dir/services/" >> "$output"
    if [[ ${#available_services[@]} -gt 0 ]]; then
        echo "# Available:" >> "$output"
        for service in "${available_services[@]}"; do
            echo "#   - $service" >> "$output"
        done
    fi
    echo "services = []" >> "$output"
}

#──[Generation]─────────────────────────────────────────────────────────────

# Generate configuration.nix from a machine directory's machine.toml.
configgen::generate() {
    local machine_dir="${1:-.}"
    local machine_toml="$machine_dir/machine.toml"
    local output_file="$machine_dir/configuration.nix"

    # Check if machine.toml exists, create from template if not
    if [[ ! -f "$machine_toml" ]]; then
        echo "Creating $machine_toml from template..."
        _configgen_generate_template "$machine_toml"
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

    # Parse arrays and convert to Nix format
    local users modules
    users=$(parse_array "$machine_toml" users | to_nix_list)
    modules=$(parse_array "$machine_toml" modules | to_nix_list)

    # Paths are relative to $STAGING_DIR, which staging::materialize
    # populates by mirroring FRAMEWORK_DIR to ./framework/ and
    # CONFIG_DIR/.system/machines to ./config/machines/ verbatim — so these
    # same relative paths are also valid against the source repos.
    local user_imports=()
    while IFS= read -r user; do
        [[ -n "$user" ]] || continue
        user_imports+=("    ./config/machines/$(configgen::resolve_user "$user")")
    done < <(parse_array "$machine_toml" users)

    local module_imports=()
    while IFS= read -r module; do
        [[ -n "$module" ]] || continue
        local origin path
        read -r origin path < <(configgen::resolve_module "$module")
        if [[ "$origin" == "framework" ]]; then
            module_imports+=("    ./framework/modules/$path")
        else
            module_imports+=("    ./config/machines/$path")
        fi
    done < <(parse_array "$machine_toml" modules)

    local service_imports=()
    while IFS= read -r service; do
        [[ -n "$service" ]] || continue
        service_imports+=("    ./config/machines/$(configgen::resolve_service "$service")")
    done < <(parse_array "$machine_toml" services)

    # Combine all imports
    local all_imports
    all_imports=$(printf '%s\n' "${user_imports[@]}" "${module_imports[@]}" "${service_imports[@]}")

    # Generate configuration.nix
    cat > "$output_file" << EOF
# Auto-generated from machine.toml - DO NOT EDIT
# Paths are relative to $STAGING_DIR, where self-heal copies this file.
# Edit machine.toml instead and run: sudo nixos-rebuild switch

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
    ./framework/system.nix
    ./config/machines/$host_name/default.nix
$all_imports
  ];

  networking.hostName = hostName;
  _module.args = { inherit mainUser hostName; };
}
EOF

    echo "✓ Generated: $output_file"
}

# Generate default.nix from a machine directory's local/*.nix files. Unlike
# configgen::generate (machine.toml -> configuration.nix, declared imports),
# this discovers whatever's actually sitting in local/ — machine-private,
# always-on, undeclared config. Deterministically sorted so a rerun without
# any local/ change produces byte-identical output (no git churn).
configgen::generate_default() {
    local machine_dir="${1:-.}"
    local local_dir="$machine_dir/local"
    local output_file="$machine_dir/default.nix"

    local imports=()
    if [[ -d "$local_dir" ]]; then
        local file
        while IFS= read -r file; do
            [[ -n "$file" ]] || continue
            imports+=("    ./local/$(basename "$file")")
        done < <(find "$local_dir" -maxdepth 1 -type f -name '*.nix' -printf '%f\n' | LC_ALL=C sort)
    fi

    if [[ ${#imports[@]} -eq 0 ]]; then
        cat > "$output_file" << EOF
# Auto-generated from local/ - DO NOT EDIT
# Drop a .nix file in local/ and run: sudo nixos-rebuild switch

{ ... }:

{
  imports = [ ];
}
EOF
    else
        local imports_block
        imports_block=$(printf '%s\n' "${imports[@]}")
        cat > "$output_file" << EOF
# Auto-generated from local/ - DO NOT EDIT
# Drop a .nix file in local/ and run: sudo nixos-rebuild switch

{ ... }:

{
  imports = [
$imports_block
  ];
}
EOF
    fi

    echo "✓ Generated: $output_file"
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
        echo "Warning: jq not found — skipping the flake.lock input check" >&2
        return 0
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

# Generate CONFIG_DIR/flake.nix from CONFIG_DIR/channels.toml plus the machine
# directories under .system/machines/. Every declared nixpkgs channel becomes
# both a flake input and an overlay attribute, so a package reference always
# states its origin (stable.rofi, unstable.hyprland). Everything is emitted in
# LC_ALL=C sorted order, so a rerun with no source change is byte-identical.
configgen::generate_flake() {
    local channels_toml="$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE"
    local output_file="$CONFIG_DIR/flake.nix"

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
        overlay_block+="      $name = import $name { inherit (prev) system config; };"$'\n'
        outputs_args+=", $name"
        lock_inputs+=("$name")
    done
    for i in ${flake_names[@]+"${!flake_names[@]}"}; do
        inputs_block+="    ${flake_names[$i]}.url = \"${flake_urls[$i]}\";"$'\n'
        lock_inputs+=("${flake_names[$i]}")
    done

    local hosts_block="" machine
    while IFS= read -r machine; do
        [[ -n "$machine" ]] || continue
        hosts_block+="      $machine = mkHost \"$machine\";"$'\n'
    done < <(configgen::discover_machines)

    inputs_block="${inputs_block%$'\n'}"
    overlay_block="${overlay_block%$'\n'}"
    hosts_block="${hosts_block%$'\n'}"

    cat > "$output_file" << EOF
# Auto-generated from $CONFIGGEN_CHANNELS_FILE - DO NOT EDIT
# Edit $CONFIGGEN_CHANNELS_FILE and run: sudo nixos-rebuild switch

{
  description = "luxos machine configurations";

  inputs = {
$inputs_block
  };

  outputs = { $outputs_args, ... }@inputs:
  let
    system = "x86_64-linux";

    # Exposes every declared channel as pkgs.<channel>.<package>, so a package
    # reference always states which channel it came from. The base channel
    # aliases prev instead of re-importing itself (pkgs already is that
    # channel); the others inherit config so allowUnfree and friends carry
    # over rather than being restated here.
    channelOverlay = final: prev: {
$overlay_block
    };

    mkHost = hostName: $CONFIGGEN_BASE_INPUT_NAME.lib.nixosSystem {
      inherit system;
      # frameworkModules lets a machine module reach a framework module by
      # name (frameworkModules + "/wayland.nix") regardless of how deep the
      # machine module is nested — the two repos aren't nested relative to
      # each other, so no relative path between them can exist.
      specialArgs = { inherit inputs hostName; frameworkModules = ./framework/modules; };
      modules = [
        ./configuration.nix
        { nixpkgs.overlays = [ channelOverlay ]; }
      ];
    };
  in {
    # One entry per machine directory under .system/machines/ — enumerated,
    # never hand-maintained.
    nixosConfigurations = {
$hosts_block
    };
  };
}
EOF

    _configgen_warn_lock_drift "${lock_inputs[@]}"

    echo "✓ Generated: $output_file"
}
