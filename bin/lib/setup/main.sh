#!/usr/bin/env bash
set -euo pipefail

# Machine bring-up: scaffolds CONFIG_DIR and this host's .local/<host> tree if
# missing (prompting for main user and timezone), then hands off to
# self_heal::run — the one sequence that links, generates and stages. Setup
# never re-implements any part of it.
# The machine is always named after `hostname`, because that's what
# nixos-rebuild resolves on every later run.
# Callers must source internal/env.sh first.

SETUP_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SETUP_LIB_DIR/../flags.sh"
source "$FRAMEWORK_DIR/internal/self-heal/self-heal.sh"

SETUP_TEMPLATE_DIR="$SETUP_LIB_DIR/templates"

# Attribute in templates/user/account.nix whose "user" is replaced with the
# user's name. The template stays valid Nix on its own.
SETUP_USER_PLACEHOLDER="users.users.user ="

# Directory scaffolded in a fresh CONFIG_DIR. modules is the only shared
# folder at CONFIG_DIR root (implementation-plan.md #24).
SETUP_INITIAL_KINDS=(modules)

# Machine-private files every new host starts with, copied from templates/.
SETUP_MACHINE_TEMPLATES=(preferences.nix hardware.nix)

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

#──[Prompts]──────────────────────────────────────────────────────────────────

# Prompt for a timezone from the numbered menu; echoes the chosen zone.
_setup_prompt_timezone() {
    local i choice
    {
        echo "  Timezone:"
        for i in "${!SETUP_TIMEZONES[@]}"; do
            if [[ "$i" -eq 0 ]]; then
                echo "    $((i + 1))) ${SETUP_TIMEZONES[$i]}  (default)"
            else
                echo "    $((i + 1))) ${SETUP_TIMEZONES[$i]}"
            fi
        done
    } >&2
    read -rp "  Select [1]: " choice
    choice="${choice:-1}"
    if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= ${#SETUP_TIMEZONES[@]} )); then
        echo "${SETUP_TIMEZONES[$((choice - 1))]}"
    else
        echo "Error: invalid timezone selection '$choice'" >&2
        exit 1
    fi
}

#──[Scaffolding]──────────────────────────────────────────────────────────────

# Create CONFIG_DIR's skeleton and channels.toml, whichever is missing.
_setup_scaffold_config_dir() {
    local kind
    for kind in "${SETUP_INITIAL_KINDS[@]}"; do
        mkdir -p "$CONFIG_DIR/$kind"
    done
    mkdir -p "$LOCAL_DIR"

    local channels="$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE"
    if [[ ! -f "$channels" ]]; then
        cp "$SETUP_TEMPLATE_DIR/$CONFIGGEN_CHANNELS_FILE" "$channels"
        echo "  Created $channels"
    fi
}

# Ensure a user module exists (<name>.nix or <name>/default.nix under
# CONFIG_DIR/modules/users), offering to scaffold one from the template if
# not. Per implementation-plan.md #19, the scaffolded shape is a folder unit
# whose default.nix only imports ./account.nix, which declares a literal
# users.users.<name> — the name is filled in at creation time, not derived
# from the folder, so renaming the folder later can't silently orphan the
# account.
_setup_ensure_user() {
    local name="$1"
    local users_dir="$CONFIG_DIR/$CONFIGGEN_MODULES_DIR/users"

    if [[ -f "$users_dir/$name.nix" || -f "$users_dir/$name/default.nix" ]]; then
        return 0
    fi

    local reply
    read -rp "  User '$name' has no config in $users_dir. Create it from template? [Y/n] " reply
    if [[ "$reply" =~ ^[Nn]$ ]]; then
        echo "Error: cannot continue without a user config for '$name'." >&2
        exit 1
    fi

    local dest="$users_dir/$name"
    mkdir -p "$dest"
    cp "$SETUP_TEMPLATE_DIR/user/default.nix" "$dest/default.nix"

    local template
    template="$(< "$SETUP_TEMPLATE_DIR/user/account.nix")"
    if [[ "$template" != *"$SETUP_USER_PLACEHOLDER"* ]]; then
        echo "Error: $SETUP_TEMPLATE_DIR/user/account.nix has no '$SETUP_USER_PLACEHOLDER' to fill in" >&2
        exit 1
    fi
    printf '%s\n' "${template//"$SETUP_USER_PLACEHOLDER"/users.users.$name =}" > "$dest/account.nix"
    echo "  Created $dest/{default.nix,account.nix} (edit groups / packages / ssh keys before building)"
}

# Scaffold .local/<host>: machine.toml plus the machine-private templates,
# and a modules/default.nix that already selects the main user (self_heal::
# run would otherwise scaffold it empty, and a plain kind-loop can't append a
# selection — this host's entrypoint has to exist first).
_setup_create_machine() {
    local host="$1"
    local dir="$LOCAL_DIR/$host"

    echo "Creating machine: $host"
    mkdir -p "$dir"

    local user_in main_user
    read -rp "  Main user [$(whoami)]: " user_in
    main_user="${user_in:-$(whoami)}"
    _setup_ensure_user "$main_user"

    local time_zone
    time_zone="$(_setup_prompt_timezone)"

    cat > "$dir/machine.toml" << EOF
# NixOS Machine Configuration - $host

hostName = "$host"
timeZone = "$time_zone"
locale = "$SETUP_DEFAULT_LOCALE"
stateVersion = "$SETUP_DEFAULT_STATE_VERSION"
EOF

    local file
    for file in "${SETUP_MACHINE_TEMPLATES[@]}"; do
        cp "$SETUP_TEMPLATE_DIR/$file" "$dir/$file"
    done

    local modules_entry="$dir/$CONFIGGEN_MODULES_DIR/default.nix"
    mkdir -p "$(dirname "$modules_entry")"
    printf '{ ... }:\n{\n  imports = [];\n}\n' > "$modules_entry"
    imports::add "$modules_entry" "users/$main_user"

    echo "  Machine '$host' scaffolded at $dir"
}

#──[Entrypoint]───────────────────────────────────────────────────────────────

setup::run() {
    local -A opts=()
    local -a rest=()
    flags::parse opts rest "dry-run:bool" "$@"

    local host machine_toml
    host="$(hostname)"
    machine_toml="$LOCAL_DIR/$host/machine.toml"

    if [[ -n "${opts[dry-run]:-}" ]]; then
        echo "[dry-run] Nothing will be written. Would:"
        [[ -f "$CONFIG_DIR/$CONFIGGEN_CHANNELS_FILE" ]] \
            || echo "  scaffold $CONFIG_DIR (${SETUP_INITIAL_KINDS[*]}, $CONFIGGEN_CHANNELS_FILE)"
        [[ -f "$machine_toml" ]] \
            || echo "  scaffold machine '$host' at $LOCAL_DIR/$host (prompting for user and timezone)"
        echo "  run self-heal for '$host' (links, generation, staging at $STAGING_DIR, /etc/nixos)"
        return 0
    fi

    _setup_scaffold_config_dir

    if [[ ! -f "$machine_toml" ]]; then
        echo "No machine config for hostname '$host'."
        local reply
        read -rp "Create one? [Y/n] " reply
        if [[ "$reply" =~ ^[Nn]$ ]]; then
            echo "Error: nixos-rebuild resolves the machine from hostname; '$host' must exist." >&2
            exit 1
        fi
        _setup_create_machine "$host"
    fi

    self_heal::run "$(configgen::resolve_machine "$host")"

    echo ""
    echo "Done. Run 'sudo nixos-rebuild test' to verify."
}
