package ui

import (
	"fmt"
	"os"
	"strings"
)

var logoLines = []string{
	"██╗     ██╗   ██╗██╗  ██╗ ██████╗ ███████╗",
	"██║     ██║   ██║╚██╗██╔╝██╔═══██╗██╔════╝",
	"██║     ██║   ██║ ╚███╔╝ ██║   ██║███████╗",
	"██║     ██║   ██║ ██╔██╗ ██║   ██║╚════██║",
	"███████╗╚██████╔╝██╔╝ ██╗╚██████╔╝███████║",
	"╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝",
}

const logoFooter = "                          made by me <3 (luar)"

const trueColorFormat = "\x1b[38;2;%d;%d;%dm"

// Gradient endpoints: green (#CCF391) to purple (#B5A6FA).
var (
	logoFrom = [3]int{0xCC, 0xF3, 0x91}
	logoTo   = [3]int{0xB5, 0xA6, 0xFA}
)

// Logo returns the luxos banner: a green-to-purple gradient on a color
// terminal, plain text otherwise.
func Logo(f *os.File) string {
	if PaletteFor(f) == (Palette{}) {
		return plainLogo()
	}
	return gradientLogo()
}

func plainLogo() string {
	return "\n" + strings.Join(logoLines, "\n") + "\n" + logoFooter + "\n\n"
}

// gradientLogo renders logoLines as a flat-per-row, top-to-bottom gradient.
func gradientLogo() string {
	span := max(len(logoLines)-1, 1)

	var b strings.Builder
	b.WriteByte('\n')
	for y, line := range logoLines {
		r := logoFrom[0] + (logoTo[0]-logoFrom[0])*y/span
		g := logoFrom[1] + (logoTo[1]-logoFrom[1])*y/span
		bl := logoFrom[2] + (logoTo[2]-logoFrom[2])*y/span
		fmt.Fprintf(&b, trueColorFormat+"%s", r, g, bl, line)
		b.WriteString(codeReset + "\n")
	}
	b.WriteString(codeLine + logoFooter + codeReset + "\n\n")
	return b.String()
}
