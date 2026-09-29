package shared

import (
	"bufio"
	"fmt"
	"os"
	"strings"
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

const noColorEnv = "NO_COLOR"

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func readLine() string {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

func isYes(reply string) bool {
	return reply == "y" || reply == "Y"
}

// An empty line or EOF returns defYes.
func Confirm(prompt string, defYes bool) (bool, error) {
	fmt.Print(prompt)

	reply := readLine()
	if reply == "" {
		return defYes, nil
	}
	return isYes(reply), nil
}

func ConfirmHardwareOff() (bool, error) {
	art := hardwareOffArt
	warning := warningLead + warningHighlighted + warningTail + warningAside
	if isCharDevice(os.Stderr) && os.Getenv(noColorEnv) == "" {
		art = strings.Replace(art, areYouSure, ansiAreYouSure+areYouSure+ansiReset, 1)
		warning = warningLead + ansiRed + ansiUnderline + warningHighlighted + ansiReset +
			warningTail + warningAside
	}
	if _, err := fmt.Fprint(os.Stderr, warning+"\n\n"+art+"\n\n"+hardwareOffPrompt); err != nil {
		return false, err
	}
	return isYes(readLine()), nil
}
