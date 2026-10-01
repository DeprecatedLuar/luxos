package templates

import (
	"io/fs"
	"strings"
	"testing"
)

func TestFile(t *testing.T) {
	for _, name := range []string{"framework/system.nix", "starters/environment", "starters/user/account.nix", "modules/desktop.nix"} {
		if _, err := File(name); err != nil {
			t.Errorf("File(%q): %v", name, err)
		}
	}
}

func TestDir(t *testing.T) {
	d, err := Dir("modules")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(d, "x11.nix"); err != nil {
		t.Errorf("modules/x11.nix: %v", err)
	}
}

func TestRender(t *testing.T) {
	out, err := Render("configuration.nix.tmpl", struct{ Host string }{"box"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "box") {
		t.Errorf("host not rendered:\n%s", out)
	}
}
