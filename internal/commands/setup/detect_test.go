package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect_ReadsSystem(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "etc/os-release"), "NAME=NixOS\nVERSION_ID=\"25.11\"\nBUILD_ID=\"25.11.20260630.b6018f8\"\n")
	mustWrite(t, filepath.Join(root, "etc/locale.conf"), "LANG=pt_PT.UTF-8\n")
	mustWrite(t, filepath.Join(root, "etc/X11/xorg.conf.d/00-keyboard.conf"),
		"Section \"InputClass\"\n  Option \"XkbModel\" \"pc104\"\n  Option \"XkbLayout\" \"pt\"\n  Option \"XkbVariant\" \"\"\nEndSection\n")
	if err := os.Symlink("/etc/zoneinfo/Europe/Lisbon", filepath.Join(root, "etc/localtime")); err != nil {
		t.Fatal(err)
	}

	got, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	want := System{
		Values:  config.MachineValues{Channel: "nixos-25.11", Timezone: "Europe/Lisbon", Locale: "pt_PT.UTF-8", Keyboard: "pt"},
		Release: "25.11",
	}
	if got != want {
		t.Errorf("Detect = %+v\nwant %+v", got, want)
	}
}

func TestDetect_StoreZoneinfoTarget(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "etc/os-release"), "VERSION_ID=25.11\n")
	if err := os.Symlink("/nix/store/abc-tzdata-2026b/share/zoneinfo/America/Sao_Paulo", filepath.Join(root, "etc/localtime")); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values.Timezone != "America/Sao_Paulo" {
		t.Errorf("Timezone = %q", got.Values.Timezone)
	}
}

func TestDetect_Fallbacks(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "etc/os-release"), "VERSION_ID=25.11\n")
	got, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	d := config.MachineDefaults
	if got.Values.Timezone != d.Timezone || got.Values.Locale != d.Locale || got.Values.Keyboard != d.Keyboard {
		t.Errorf("fallbacks = %+v, want defaults %+v", got.Values, d)
	}
	if got.Values.Channel != "nixos-25.11" {
		t.Errorf("Channel = %q", got.Values.Channel)
	}
}

func TestDetect_MissingReleaseIsError(t *testing.T) {
	root := t.TempDir()
	if _, err := Detect(root); err == nil {
		t.Error("missing os-release: want error")
	}
	mustWrite(t, filepath.Join(root, "etc/os-release"), "NAME=NixOS\n")
	_, err := Detect(root)
	if err == nil || !strings.Contains(err.Error(), "VERSION_ID") {
		t.Errorf("missing VERSION_ID: %v", err)
	}
}
