#!/usr/bin/env bash
# Sets up NixOS symlinks for this machine.
# Run once on a new machine after checking out the luxos framework + config.
# If the machine is unknown, offers to scaffold a new machine (and user).
#
# Usage: setup.sh [--dry-run]
#   --dry-run  Scaffold/resolve the machine, then stop before any sudo or
#              symlink changes. Prints what it *would* link. Nothing outside
#              CONFIG_DIR's machines/ and users/ is touched. Safe to test with.

set -e

DRY_RUN=""
if [ "${1:-}" = "--dry-run" ]; then
  DRY_RUN=1
  echo "[dry-run] No sudo or symlink changes will be made."
fi

FRAMEWORK_DIR="$HOME/Workspace/dev/luxos"
CONFIG_DIR="$HOME/.config/luxos"

source "$FRAMEWORK_DIR/scripts/lib/common.sh"

MACHINES_DIR="$CONFIG_DIR/machines"
USERS_DIR="$CONFIG_DIR/users"
GENERATE_CONFIG="$FRAMEWORK_DIR/scripts/generate-config.sh"
USER_TEMPLATE="$USERS_DIR/user.nix"
TEMPLATE_DIR="$FRAMEWORK_DIR/scripts/setup"
HOST="$(hostname)"

# Defaults for scaffolded machines
DEFAULT_LOCALE="en_US.UTF-8"
DEFAULT_STATE_VERSION="25.05"
COMPOSITOR_OPTIONS="hyprland niri xfce i3 openbox"

# Timezone menu (first entry is the default)
TIMEZONES=(
  "America/Sao_Paulo"
  "Europe/London"
  "UTC"
  "America/New_York"
  "Europe/Berlin"
)

#──[Config scaffolding]────────────────────────────────────────────────────────

# First-time checkout: CONFIG_DIR doesn't exist yet, create its skeleton.
scaffold_config_dir() {
  echo "No config directory found at $CONFIG_DIR, creating it..."
  mkdir -p "$MACHINES_DIR" "$USERS_DIR" "$CONFIG_DIR/services/available"
}

if [ ! -d "$CONFIG_DIR" ]; then
  scaffold_config_dir
fi

#──[Helpers]──────────────────────────────────────────────────────────────────

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
    for i in "${!TIMEZONES[@]}"; do
      if [ "$i" -eq 0 ]; then
        echo "    $((i + 1))) ${TIMEZONES[$i]}  (default)"
      else
        echo "    $((i + 1))) ${TIMEZONES[$i]}"
      fi
    done
  } >&2
  read -rp "  Select [1]: " choice
  choice="${choice:-1}"
  if [[ "$choice" =~ ^[0-9]+$ ]] && [ "$choice" -ge 1 ] && [ "$choice" -le "${#TIMEZONES[@]}" ]; then
    echo "${TIMEZONES[$((choice - 1))]}"
  else
    echo "${TIMEZONES[0]}"
  fi
}

# Scaffold a new user file from the template, swapping in the username.
create_user() {
  local name="$1"
  sed "s/users\.users\.user =/users.users.$name =/" "$USER_TEMPLATE" > "$USERS_DIR/$name.nix"
  echo "  Created user: $USERS_DIR/$name.nix"
  echo "    (edit groups / packages / ssh key before building)"
}

# Ensure a user file exists, offering to scaffold one if not.
resolve_user() {
  local name="$1"
  if [ -f "$USERS_DIR/$name.nix" ]; then
    return 0
  fi
  echo "  User '$name' has no config in $USERS_DIR." >&2
  read -rp "  Create it from template? [Y/n] " reply
  if [[ "$reply" =~ ^[Nn]$ ]]; then
    echo "  Error: cannot continue without a user config for '$name'." >&2
    exit 1
  fi
  create_user "$name" >&2
}

# Scaffold a new machine directory, then generate its configuration.nix.
create_machine() {
  local machine="$1"
  local dir="$MACHINES_DIR/$machine"

  if [ -d "$dir" ]; then
    echo "Error: machine '$machine' already exists at $dir"
    exit 1
  fi

  echo "Creating new machine: $machine"

  # hostName == machine name (dir name is the network hostname)
  local host_name="$machine"

  # main user (scaffold if missing)
  read -rp "  Main user [$(whoami)]: " user_in
  local main_user="${user_in:-$(whoami)}"
  resolve_user "$main_user"

  # compositors (space-separated, empty = headless)
  echo "  Compositors (space-separated, blank for headless)"
  echo "    Options: $COMPOSITOR_OPTIONS"
  read -rp "  > " compositors_in

  # modules (space-separated, empty = none)
  local available_modules
  available_modules="$(discover_modules)"
  echo "  Modules (space-separated, blank for none)"
  echo "    Available: ${available_modules:-none}"
  read -rp "  > " modules_in

  # timezone
  local time_zone
  time_zone="$(prompt_timezone)"

  mkdir -p "$dir"

  # machine.toml (source of truth)
  cat > "$dir/machine.toml" << EOF
# NixOS Machine Configuration - $machine

# Users to import (first user is main user)
users = ["$main_user"]
hostName = "$host_name"
timeZone = "$time_zone"
locale = "$DEFAULT_LOCALE"
stateVersion = "$DEFAULT_STATE_VERSION"

# Desktop compositors
compositors = [$(to_toml_array "$compositors_in")]

# Modules to import
modules = [$(to_toml_array "$modules_in")]
EOF

  # Static skeleton files (default / preferences / services / hardware)
  cp "$TEMPLATE_DIR"/{default,preferences,services,hardware}.nix "$dir/"

  # Generate configuration.nix from the toml
  "$GENERATE_CONFIG" "$dir"

  echo "Machine '$machine' scaffolded."
}

#──[Resolve machine]──────────────────────────────────────────────────────────

if [ -d "$MACHINES_DIR/$HOST" ]; then
  MACHINE="$HOST"
else
  echo "No machine config found for hostname '$HOST'."
  echo ""
  echo "Available machines:"
  ls "$MACHINES_DIR"
  echo ""
  read -rp "Create a new machine config? [Y/n] " reply
  if [[ "$reply" =~ ^[Nn]$ ]]; then
    read -rp "Enter existing machine name: " MACHINE
    if [ ! -d "$MACHINES_DIR/$MACHINE" ]; then
      echo "Error: '$MACHINE' not found in $MACHINES_DIR"
      exit 1
    fi
  else
    read -rp "New machine name [$HOST]: " MACHINE
    MACHINE="${MACHINE:-$HOST}"
    create_machine "$MACHINE"
  fi
fi

echo "Setting up for machine: $MACHINE"

MAIN_USER=$(parse_main_user "$MACHINES_DIR/$MACHINE/configuration.nix")
if [ -z "$MAIN_USER" ]; then
  echo "Error: Could not parse mainUser from configuration.nix"
  exit 1
fi

echo "  Main user: $MAIN_USER"

if [ -n "$DRY_RUN" ]; then
  echo ""
  echo "[dry-run] Machine '$MACHINE' resolved. Would apply (skipped):"
  echo "  heal /etc/nixos -> $MACHINES_DIR/$MACHINE/configuration.nix"
  echo "  link machine *.nix (except default.nix, services.nix, configuration.nix) + machine.toml into $CONFIG_DIR"
  echo ""
  echo "[dry-run] Done. Only $CONFIG_DIR's machines/ and users/ were written."
  exit 0
fi

ensure_etc_nixos "$MACHINES_DIR/$MACHINE/configuration.nix"

link_machine_files "$MACHINE"
echo "  Linked machine files into $CONFIG_DIR"

echo ""
echo "Done. Run 'sudo nixos-rebuild test' to verify."
