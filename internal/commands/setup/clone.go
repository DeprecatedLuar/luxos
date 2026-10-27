package setup

import (
	"regexp"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

// git comes from the luxos package (shell.PrependBundledPath).
const (
	gitBin       = "git"
	gitCloneVerb = "clone"
)

var scpURLRe = regexp.MustCompile(`^[^@\s/]+@[^:\s/]+:\S+$`)

// isRemoteURL reports whether s is an https or ssh URL, or git's scp form.
func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "ssh://") || scpURLRe.MatchString(s)
}

// Clone runs git clone url dir attached to the terminal; authentication is
// entirely git's.
func Clone(url, dir string) error {
	return shell.Run(shell.Cmd{Bin: gitBin, Args: []string{gitCloneVerb, url, dir}})
}
