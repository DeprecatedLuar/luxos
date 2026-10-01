package shared

import (
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	warningLead        = "You are about to "
	warningHighlighted = "disable the hardware support"
	warningTail        = " of your machine"
	warningAside       = " (scary)"
)

const hardwareOffArt = `            ⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
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

const hardwareOffPrompt = "Proceed? [y/N] "

func ConfirmHardwareOff() (bool, error) {
	art := hardwareOffArt
	warning := warningLead + warningHighlighted + warningTail + warningAside
	if ui.PaletteFor(os.Stderr) != (ui.Palette{}) {
		art = strings.Replace(art, areYouSure, ansiAreYouSure+areYouSure+ansiReset, 1)
		warning = warningLead + ansiRed + ansiUnderline + warningHighlighted + ansiReset +
			warningTail + warningAside
	}
	reply, err := ui.Ask(os.Stderr, warning+"\n\n"+art+"\n\n"+hardwareOffPrompt)
	if err != nil {
		return false, err
	}
	return ui.IsYes(reply), nil
}
