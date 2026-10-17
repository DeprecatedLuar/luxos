package shared

import (
	"fmt"
	"io"

	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const keyNotice = "\nSave this random key somewhere safe, you will need it on every computer that imports %s:\n\n%s\n"

// KeyNotice shows user's new key: the one thing to save from its creation.
func KeyNotice(w io.Writer, user, secret string) {
	pal := ui.PaletteOf(w)
	ui.Print(w, fmt.Sprintf(keyNotice, pal.Name(user), pal.Secret(secret)))
}
