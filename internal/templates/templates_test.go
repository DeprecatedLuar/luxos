package templates

import (
	"io/fs"
	"strings"
	"testing"
)

func TestFile(t *testing.T) {
	for _, name := range []string{"framework/system.nix", "starters/environment", "starters/user/default.nix", "modules/desktop.nix"} {
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
	out, err := Render("configuration.nix.tmpl", struct {
		Host     string
		Settings []string
	}{"box", []string{"../config/settings/laptop.nix"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"box"`, "./system.nix", "./hardware-facts.nix", "./luxos-hardware.nix", "../config/modules", "    ../config/settings/laptop.nix\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("configuration.nix missing %q:\n%s", want, out)
		}
	}
}

func TestSystemNix_ImportsNothing(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sys), "imports") {
		t.Error("system.nix must not import; configuration.nix holds every import")
	}
}

func TestSystemNix_RetentionInMachineTemplate(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sys), "nix.gc") {
		t.Error("system.nix must not define nix.gc")
	}
	for _, want := range []string{
		"nix.optimise.automatic = lib.mkDefault true;",
		"nix.settings.auto-optimise-store = lib.mkDefault true;",
	} {
		if !strings.Contains(string(sys), want) {
			t.Errorf("system.nix missing %q", want)
		}
	}
}

func TestSystemNix_ImportsNoHardwareFiles(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sys), "config/local/hardware-support") {
		t.Error("system.nix must not import hardware files")
	}
	if strings.Contains(string(sys), "RuntimeWatchdogSec") {
		t.Error("system.nix must not set RuntimeWatchdogSec")
	}
}
