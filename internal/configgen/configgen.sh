#!/usr/bin/env bash
set -euo pipefail

# Generates configuration.nix from a machine's machine.toml, and exposes
# discovery of the pools (users/modules/services) configuration.nix imports
# from. Callers must source internal/env.sh (FRAMEWORK_DIR, CONFIG_DIR) first.

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
    echo "machine ${match/#$CONFIG_DIR\/.system\/machines\//}"
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
    echo "${match/#$CONFIG_DIR\/.system\/machines\//}"
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
