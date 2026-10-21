package templates

import (
	"io/fs"
	"strings"
	"testing"
)

func TestFile(t *testing.T) {
	for _, name := range []string{"framework/system.nix", "starters/environment", "user/default.nix", "modules/desktop.nix"} {
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
	for _, want := range []string{`"box"`, "./system.nix", "./hardware-facts.nix", "./luxos-hardware.nix", "../config/modules", "../config/hardware-configuration.nix", "../config/host.nix", "    ../config/settings/laptop.nix\n"} {
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

func TestSystemNix_StoreOptimisation(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
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
}

func TestSystemDefaultsPanicAndGC(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`boot.kernel.sysctl."kernel.panic" = lib.mkDefault 10;`,
		`automatic = lib.mkDefault true;`,
		`dates = lib.mkDefault "weekly";`,
		`options = lib.mkDefault "--delete-older-than 14d";`,
	} {
		if !strings.Contains(string(sys), want) {
			t.Errorf("system.nix lacks %q", want)
		}
	}
	for _, gone := range []string{"panic_on_oops", "hardlockup_panic", "RuntimeWatchdogSec"} {
		if strings.Contains(string(sys), gone) {
			t.Errorf("system.nix still sets %s", gone)
		}
	}
	if _, err := File("starters/hardware.nix"); err == nil {
		t.Error("starters/hardware.nix still embedded")
	}
}

func TestAutoRebootStarter(t *testing.T) {
	src, err := File("starters/modules/hardware/auto-reboot.nix")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"kernel.panic_on_oops" = 1;`,
		`"kernel.hardlockup_panic" = 1;`,
		`systemd.settings.Manager.RuntimeWatchdogSec = "20s";`,
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("auto-reboot.nix lacks %q", want)
		}
	}
}

func TestSystemCopiesBothSelections(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"../config/modules/host/default.nix",
		"$out/luxos/modules/host/default.nix",
		"../config/modules/machine/default.nix",
		"$out/luxos/modules/machine/default.nix",
	} {
		if !strings.Contains(string(sys), want) {
			t.Errorf("system.nix lacks %q", want)
		}
	}
}

func TestSystemNix_BasePackages(t *testing.T) {
	sys, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	s := string(sys)
	for _, want := range []string{"    fastfetchMinimal\n", `environment.shellAliases.fetch = lib.mkDefault "fastfetch";`} {
		if !strings.Contains(s, want) {
			t.Errorf("system.nix lacks %q", want)
		}
	}
	for _, gone := range []string{"    ffmpeg\n", "    fastfetch\n"} {
		if strings.Contains(s, gone) {
			t.Errorf("system.nix still installs %q", strings.TrimSpace(gone))
		}
	}
}
