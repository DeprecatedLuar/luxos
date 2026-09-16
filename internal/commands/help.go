package commands

import (
	"fmt"
	"io"
)

// helpText is the top-level usage text, adapted from
// bin/lib/lux/commands/help.sh with rebuild and shell added.
const helpText = `Usage:
  luxos <command> [args]

Commands:
  help                                     Show this message
  rebuild [flags] [nixos-rebuild args]     Rebuild the system from CONFIG_DIR
  module list [category-path]              List modules (grouped by category)
  module add <category/name> [--enable]    Scaffold a module
  module edit <name>                       Open a module in $EDITOR
  module enable <name>                     Enable a module on this host
  module disable <name>                    Disable a module on this host
  module remove|rm <name> [-y]             Delete a module everywhere it's imported
  module rename|rn <old> <new>             Rename a module's identity
  user ...                                 Same verbs as module, fixed to modules/users
  shell [args]                             Exec nix-shell
`

// Help writes the top-level usage text to w.
func Help(w io.Writer) {
	fmt.Fprint(w, helpText)
}
