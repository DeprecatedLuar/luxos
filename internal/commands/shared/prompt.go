package shared

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// hardwareOffWarning heads the confirmation shown before building or
// switching without the hardware module.
const hardwareOffWarning = "You are about to disable the hardware support of your machine (scary)"

// hardwareOffArt is printed under the warning; areYouSure inside it is
// highlighted when stderr is a terminal.
const hardwareOffArt = `            ⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
                ⠀⣠⣴⣾⣶⣶⣤⣄⣄⣤⣶⣾⣿⣿⣿⣿⣿⣶⣤⢀⣀⠀⠀⠀⠀
                ⡈⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣷⢸⣿⣿⣶⡄⠀
                ⡇⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⢸⣯⣕⣶⡇⠀
                ⢰⣿⡟⠙⠿⠿⠿⣿⣹⣿⠟⠛⠛⠋⠁⠈⠻⣿⣿⣧⠹⣿⣿⠁⠀
                ⠘⣟⣤⠠⠀⠤⠀⡘⣿⣿⡷⠄⡀⢐⡒⠻⠻⣿⣿⣿⡇⠿⣫⢤⠀
                ⠃⣿⣿⣜⣋⣀⠜⡪⣸⣿⣧⣼⣇⣩⣠⣬⣁⣾⣿⣿⣿⣰⡇⣠⡀
                ⠀⣿⣿⣿⣿⣿⣿⡇⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡆⣨⠀
                ⣶⡘⣿⣿⣿⣿⣿⡇⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⢏⣼⣿⣵⠏⠀
                ⠘⣷⡸⣿⣿⣿⣿⣇⠻⠿⠛⢹⣿⣿⣿⣿⣿⣿⢃⣾⣿⠿⠏⠀⠀
                ⠀⠸⡇⣿⣿⡿⠛⠀⠁⠀⠀⠀⠉⠛⢿⣿⣿⣿⢸⣿⣿⢠⡆⠀⠀
                ⠀⠀⣇⢿⣿⠁⠀⠀⠀⢀⣀⣀⣀⠀⠀⢿⣿⡟⣸⣿⣿⢸⡇⠀⠀
  ARE YOU SURE?      ⢻⡜⣿⣴⣿⣿⣯⣛⣛⣿⣿⣿⣶⣬⣿⢣⣿⡿⠃⣠⡇⠀⠀
                ⠀⠀⠀⢻⣾⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣧⣿⠟⠀⣸⣿⣇⠀⠀
                ⠀⠀⠀⣆⢻⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡿⢃⢂⣼⣿⣿⣿⠀⠀
                ⠀⠀⠀⢿⣧⠹⣿⣿⣿⣷⣿⣿⣿⣿⣿⡟⣱⢃⣾⣿⣿⣿⣿⣄⡀
                ⠀⠀⠀⠸⠿⠷⠔⠭⠭⠭⠭⠭⠥⠶⠶⠾⠇⠾⠿⠿⠿⠿⠿⠿⠿`

// areYouSure is the text inside hardwareOffArt that gets highlighted.
const areYouSure = "ARE YOU SURE?"

// ansiAreYouSure is bold italic red; ansiReset ends it.
const (
	ansiAreYouSure = "\x1b[1;3;31m"
	ansiReset      = "\x1b[0m"
)

// hardwareOffPrompt ends the hardware confirmation.
const hardwareOffPrompt = "Proceed? [y/N] "

// noColorEnv disables the highlight when set to a non-empty value.
const noColorEnv = "NO_COLOR"

// isCharDevice reports whether f is a character device (a terminal).
func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// readLine reads one line from stdin, trimmed; "" on EOF or an empty line.
func readLine() string {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

// isYes reports whether reply is "y" or "Y".
func isYes(reply string) bool {
	return reply == "y" || reply == "Y"
}

// Confirm prints prompt and reads one line of input from stdin, returning
// true for "y"/"Y" and false for anything else. An empty line or EOF
// returns defYes.
func Confirm(prompt string, defYes bool) (bool, error) {
	fmt.Print(prompt)

	reply := readLine()
	if reply == "" {
		return defYes, nil
	}
	return isYes(reply), nil
}

// ConfirmHardwareOff warns on stderr that the hardware module will not be
// part of the system and reads one line from stdin; true only for "y"/"Y".
func ConfirmHardwareOff() (bool, error) {
	art := hardwareOffArt
	if isCharDevice(os.Stderr) && os.Getenv(noColorEnv) == "" {
		art = strings.Replace(art, areYouSure, ansiAreYouSure+areYouSure+ansiReset, 1)
	}
	if _, err := fmt.Fprint(os.Stderr, hardwareOffWarning+"\n\n"+art+"\n\n"+hardwareOffPrompt); err != nil {
		return false, err
	}
	return isYes(readLine()), nil
}
