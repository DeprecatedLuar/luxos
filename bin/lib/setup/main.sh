#!/usr/bin/env bash
set -euo pipefail

# Sets up NixOS symlinks for this machine.
# Run once on a new machine after checking out the luxos framework + config.
# If the machine is unknown, offers to scaffold a new machine (and user).
# Callers must source internal/env.sh first.

SETUP_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$FRAMEWORK_DIR/internal/links/links.sh"
source "$FRAMEWORK_DIR/internal/configgen/configgen.sh"
source "$FRAMEWORK_DIR/internal/staging/staging.sh"

SETUP_TEMPLATE_DIR="$SETUP_LIB_DIR/templates"

# Defaults for scaffolded machines
SETUP_DEFAULT_LOCALE="en_US.UTF-8"
SETUP_DEFAULT_STATE_VERSION="25.05"

# Timezone menu (first entry is the default)
SETUP_TIMEZONES=(
  "America/Sao_Paulo"
  "Europe/London"
  "UTC"
  "America/New_York"
  "Europe/Berlin"
)

#──[Helpers]──────────────────────────────────────────────────────────────────
# Private to this package — unprefixed.

# Parse mainUser out of a generated configuration.nix
parse_main_user() {
    local config_file="$1"
    awk '/mainUser = / {match($0, /"([^"]+)"/, arr); print arr[1]}' "$config_file"
}

# Convert a space-separated list into a TOML array body: a b -> "a", "b"
to_toml_array() {
  local out="" item
  for item in $1; do
    if [ -n "$out" ]; then out="$out, "; fi
    out="$out\"$item\""
  done
  echo "$out"
}

# Prompt for a timezone from the numbered menu; echoes the chosen zone.
prompt_timezone() {
  local i choice
  {
    echo "  Timezone:"
    for i in "${!SETUP_TIMEZONES[@]}"; do
      if [ "$i" -eq 0 ]; then
        echo "    $((i + 1))) ${SETUP_TIMEZONES[$i]}  (default)"
      else
        echo "    $((i + 1))) ${SETUP_TIMEZONES[$i]}"
      fi
    done
  } >&2
  read -rp "  Select [1]: " choice
  choice="${choice:-1}"
  if [[ "$choice" =~ ^[0-9]+$ ]] && [ "$choice" -ge 1 ] && [ "$choice" -le "${#SETUP_TIMEZONES[@]}" ]; then
    echo "${SETUP_TIMEZONES[$((choice - 1))]}"
  else
    echo "${SETUP_TIMEZONES[0]}"
  fi
}

#──[Config scaffolding]────────────────────────────────────────────────────────

# First-time checkout: CONFIG_DIR doesn't exist yet, create its skeleton.
setup::_scaffold_config_dir() {
  local system_dir="$1"
  local machines_dir="$2"
  echo "No config directory found at $CONFIG_DIR, creating it..."
  mkdir -p "$machines_dir" "$system_dir/users" "$system_dir/services" "$system_dir/modules"
  cp "$SETUP_TEMPLATE_DIR/$CONFIGGEN_CHANNELS_FILE" "$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE"
}

# Scaffold a new user file, authored inside the machine that owns it.
setup::create_user() {
  local name="$1"
  local machine_dir="$2"
  local user_template="$3"
  local dest="$machine_dir/users/$name.nix"

  mkdir -p "$machine_dir/users"
  if [ -f "$user_template" ]; then
    sed "s/users\.users\.user =/users.users.$name =/" "$user_template" > "$dest"
  else
    echo "  Error: no user template in the pool ($user_template)." >&2
    exit 1
  fi
  echo "  Created user: $dest"
  echo "    (edit groups / packages / ssh key before building)"
}

# Ensure the user resolves from the pool, offering to scaffold one if not.
setup::resolve_user() {
  local name="$1"
  local machine_dir="$2"
  local system_dir="$3"
  if [ -e "$system_dir/users/$name.nix" ]; then
    return 0
  fi
  echo "  User '$name' has no config in $system_dir/users." >&2
  read -rp "  Create it from template? [Y/n] " reply
  if [[ "$reply" =~ ^[Nn]$ ]]; then
    echo "  Error: cannot continue without a user config for '$name'." >&2
    exit 1
  fi
  setup::create_user "$name" "$machine_dir" "$system_dir/users/user.nix" >&2
  links::build_all_pools
}

# Scaffold a new machine directory, then generate its configuration.nix.
setup::create_machine() {
  local machine="$1"
  local machines_dir="$2"
  local dir="$machines_dir/$machine"

  if [ -d "$dir" ]; then
    echo "Error: machine '$machine' already exists at $dir"
    exit 1
  fi

  echo "Creating new machine: $machine"

  # hostName == machine name (dir name is the network hostname)
  local host_name="$machine"

  mkdir -p "$dir/users" "$dir/services" "$dir/modules" "$dir/local"

  # main user (scaffold if missing)
  read -rp "  Main user [$(whoami)]: " user_in
  local main_user="${user_in:-$(whoami)}"
  setup::resolve_user "$main_user" "$dir" "$CONFIG_DIR/.system"

  # modules (space-separated, empty = none)
  local available_modules
  available_modules="$(configgen::discover_modules)"
  echo "  Modules (space-separated, blank for none)"
  echo "    Available: ${available_modules:-none}"
  read -rp "  > " modules_in

  # timezone
  local time_zone
  time_zone="$(prompt_timezone)"

  # machine.toml (source of truth)
  cat > "$dir/machine.toml" << EOF
# NixOS Machine Configuration - $machine

# Users to import (first user is main user)
users = ["$main_user"]
hostName = "$host_name"
timeZone = "$time_zone"
locale = "$SETUP_DEFAULT_LOCALE"
stateVersion = "$SETUP_DEFAULT_STATE_VERSION"

# Modules to import
modules = [$(to_toml_array "$modules_in")]

# Services to import
services = []
EOF

  # Static skeleton files (preferences / hardware) — always-on, machine-private
  cp "$SETUP_TEMPLATE_DIR"/{preferences,hardware}.nix "$dir/local/"
  links::build_all_pools

  # Generate configuration.nix from the toml, and default.nix from local/
  configgen::generate "$dir"
  configgen::generate_default "$dir"

  echo "Machine '$machine' scaffolded."
}

#──[Entrypoint]─────────────────────────────────────────────────────────────

setup::run() {
  local dry_run=""
  if [ "${1:-}" = "--dry-run" ]; then
    dry_run=1
    echo "[dry-run] No sudo or symlink changes will be made."
  fi

  local system_dir="$CONFIG_DIR/.system"
  local machines_dir="$system_dir/machines"
  local host
  host="$(hostname)"

  if [ ! -d "$CONFIG_DIR" ]; then
    setup::_scaffold_config_dir "$system_dir" "$machines_dir"
  elif [ ! -f "$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE" ]; then
    echo "No $CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE found, scaffolding it..."
    cp "$SETUP_TEMPLATE_DIR/$CONFIGGEN_CHANNELS_FILE" "$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE"
  fi

  links::build_all_pools

  local machine
  if [ -d "$machines_dir/$host" ]; then
    machine="$host"
  else
    echo "No machine config found for hostname '$host'."
    echo ""
    echo "Available machines:"
    ls "$machines_dir"
    echo ""
    read -rp "Create a new machine config? [Y/n] " reply
    if [[ "$reply" =~ ^[Nn]$ ]]; then
      read -rp "Enter existing machine name: " machine
      if [ ! -d "$machines_dir/$machine" ]; then
        echo "Error: '$machine' not found in $machines_dir"
        exit 1
      fi
    else
      read -rp "New machine name [$host]: " machine
      machine="${machine:-$host}"
      setup::create_machine "$machine" "$machines_dir"
    fi
  fi

  echo "Setting up for machine: $machine"

  local main_user
  main_user=$(parse_main_user "$machines_dir/$machine/configuration.nix")
  if [ -z "$main_user" ]; then
    echo "Error: Could not parse mainUser from configuration.nix"
    exit 1
  fi

  echo "  Main user: $main_user"

  if [ -n "$dry_run" ]; then
    echo ""
    echo "[dry-run] Machine '$machine' resolved. Would apply (skipped):"
    echo "  generate $CONFIG_DIR/flake.nix from $CONFIGGEN_CHANNELS_FILE"
    echo "  materialize $STAGING_DIR from $machines_dir/$machine/"
    echo "  heal /etc/nixos (hardware-configuration.nix + env only)"
    echo "  link machine *.nix (except default.nix, configuration.nix) + machine.toml + local/ + modules/ + services/ into $CONFIG_DIR"
    echo ""
    echo "[dry-run] Done. Only $system_dir was written."
    return 0
  fi

  # Regenerated here, not at scaffold time: a machine created above must
  # appear in nixosConfigurations before staging copies flake.nix.
  configgen::generate_flake

  staging::materialize "$machines_dir/$machine"
  links::ensure_etc_nixos

  links::link_machine_files "$machine"
  echo "  Linked machine files into $CONFIG_DIR"

  echo ""
  echo "Done. Run 'sudo nixos-rebuild test' to verify."
}
