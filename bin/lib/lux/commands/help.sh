#!/usr/bin/env bash
# lux help - top-level usage text

cmd_help() {
    cat <<'EOF'
Usage:
  lux <command> [args]

Commands:
  help                                     Show this message
  module list [category-path]              List modules (grouped by category)
  module add <category/name> [--enable]    Scaffold a module
  module edit <name>                       Open a module in $EDITOR
  module enable <name>                     Enable a module on this host
  module disable <name>                    Disable a module on this host
  module remove|rm <name> [-y]             Delete a module everywhere it's imported
  module rename|rn <old> <new>             Rename a module's identity
  user ...                                 Same verbs as module, fixed to modules/users
EOF
}
