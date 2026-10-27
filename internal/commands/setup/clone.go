package setup

import "github.com/DeprecatedLuar/luxos/internal/shell"

// git comes from the luxos package (shell.PrependBundledPath).
const (
	gitBin       = "git"
	gitCloneVerb = "clone"
)

// Clone runs git clone url dir attached to the terminal; authentication is
// entirely git's.
func Clone(url, dir string) error {
	return shell.Run(shell.Cmd{Bin: gitBin, Args: []string{gitCloneVerb, url, dir}})
}
