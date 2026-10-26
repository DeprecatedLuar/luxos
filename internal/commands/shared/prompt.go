package shared

import (
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	warningLead        = "You are about to "
	warningHighlighted = "build without boot.nix"
	warningTail        = " on this computer"
	warningAside       = " (scary)"
)

const bootOffArt = `            ⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
                ⠀⣠⣴⣾⣶⣶⣤⣄⣄⣤⣶⣾⣿⣿⣿⣿⣿⣶⣤⢀⣀⠀⠀⠀⠀
                ⡈⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣷⢸⣿⣿⣶⡄⠀
                ⡇⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⢸⣯⣕⣶⡇⠀
                ⢰⣿⡟⠙⠿⠿⠿⣿⣹⣿⠟⠛⠛⠋⠁⠈⠻⣿⣿⣧⠹⣿⣿⠁⠀
                ⠘⣟⣤⠠⠀⠤⠀⡘⣿⣿⡷⠄⡀⢐⡒⠻⠻⣿⣿⣿⡇⠿⣫⢤⠀
                ⠃⣿⣿⣜⣋⣀⠜⡪⣸⣿⣧⣼⣇⣩⣠⣬⣁⣾⣿⣿⣿⣰⡇⣠⡀
                ⠀⣿⣿⣿⣿⣿⣿⡇⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡆⣨⠀
                ⣶⡘⣿⣿⣿⣿⣿⡇⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⢏⣼⣿⣵⠏⠀
 ARE YOU SURE?  ⠘⣷⡸⣿⣿⣿⣿⣇⠻⠿⠛⢹⣿⣿⣿⣿⣿⣿⢃⣾⣿⠿⠏⠀⠀
                ⠀⠸⡇⣿⣿⡿⠛⠀⠁⠀⠀⠀⠉⠛⢿⣿⣿⣿⢸⣿⣿⢠⡆⠀⠀
                 ⠀⣇⢿⣿⠁⠀⠀⠀⢀⣀⣀⣀⠀⠀⢿⣿⡟⣸⣿⣿⢸⡇⠀⠀
                  ⢻⡜⣿⣴⣿⣿⣯⣛⣛⣿⣿⣿⣶⣬⣿⢣⣿⡿⠃⣠⡇⠀⠀
                ⠀⠀⠀⢻⣾⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣧⣿⠟⠀⣸⣿⣇⠀⠀
                ⠀⠀⠀⣆⢻⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡿⢃⢂⣼⣿⣿⣿⠀⠀
                ⠀⠀⠀⢿⣧⠹⣿⣿⣿⣷⣿⣿⣿⣿⣿⡟⣱⢃⣾⣿⣿⣿⣿⣄⡀
                ⠀⠀⠀⠸⠿⠷⠔⠭⠭⠭⠭⠭⠥⠶⠶⠾⠇⠾⠿⠿⠿⠿⠿⠿⠿`

const areYouSure = "ARE YOU SURE?"

const (
	ansiAreYouSure = "\x1b[1;3;31m"
	ansiRed        = "\x1b[31m"
	ansiUnderline  = "\x1b[4m"
	ansiReset      = "\x1b[0m"
)

// ProceedPrompt asks a yes/no question that defaults to no.
const ProceedPrompt = "Proceed? [y/N] "

// ConfirmBootOff asks before a build that leaves the boot loader module out.
func ConfirmBootOff() (bool, error) {
	art := bootOffArt
	warning := warningLead + warningHighlighted + warningTail + warningAside
	if ui.PaletteFor(os.Stderr) != (ui.Palette{}) {
		art = strings.Replace(art, areYouSure, ansiAreYouSure+areYouSure+ansiReset, 1)
		warning = warningLead + ansiRed + ansiUnderline + warningHighlighted + ansiReset +
			warningTail + warningAside
	}
	reply, err := ui.Ask(os.Stderr, warning+"\n\n"+art+"\n\n"+ProceedPrompt)
	if err != nil {
		return false, err
	}
	return ui.IsYes(reply), nil
}
