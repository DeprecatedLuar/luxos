package setup

import "github.com/DeprecatedLuar/luxos/internal/shell"

// Fresh NixOS has no git and no flakes enabled, so git comes from nixpkgs for
// this one call.
const (
	nixBin       = "nix"
	nixFeatures  = "nix-command flakes"
	gitInstall   = "nixpkgs#git"
	gitBin       = "git"
	gitCloneVerb = "clone"
)

// Clone runs git clone url dir attached to the terminal; authentication is
// entirely git's.
func Clone(url, dir string) error {
	return shell.Run(shell.Cmd{Bin: nixBin, Args: []string{
		"--extra-experimental-features", nixFeatures,
		"shell", gitInstall, "-c", gitBin, gitCloneVerb, url, dir,
	}})
}
