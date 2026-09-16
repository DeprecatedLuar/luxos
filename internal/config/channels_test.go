package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func templateChannelsToml(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../framework/files/templates/channels.toml")
	if err != nil {
		t.Fatalf("reading channels.toml template: %v", err)
	}
	return data
}

func TestLoadChannels_ValidTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	if err := os.WriteFile(path, templateChannelsToml(t), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadChannels(path)
	if err != nil {
		t.Fatalf("LoadChannels: %v", err)
	}
	if c.Base != "stable" {
		t.Fatalf("Base = %q, want stable", c.Base)
	}
	wantChannels := []Input{
		{Name: "stable", URL: "github:NixOS/nixpkgs/nixos-25.11"},
		{Name: "unstable", URL: "github:NixOS/nixpkgs/nixpkgs-unstable"},
	}
	if len(c.Channels) != len(wantChannels) {
		t.Fatalf("Channels = %+v, want %+v", c.Channels, wantChannels)
	}
	for i, want := range wantChannels {
		if c.Channels[i] != want {
			t.Errorf("Channels[%d] = %+v, want %+v", i, c.Channels[i], want)
		}
	}
	if len(c.Flakes) != 0 {
		t.Fatalf("Flakes = %+v, want none", c.Flakes)
	}
}

func TestLoadChannels_FileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), path+" not found") {
		t.Errorf("error %q missing file mention", err.Error())
	}
	if !strings.Contains(err.Error(), "create it from templates/channels.toml in the luxos repo") {
		t.Errorf("error %q missing hint", err.Error())
	}
}

func TestLoadChannels_UnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "stable"
extra = "oops"

[channels]
stable = "github:NixOS/nixpkgs/nixos-25.11"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "unknown top-level key 'extra' in " + path
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
}

func TestLoadChannels_BaseMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
[channels]
stable = "github:NixOS/nixpkgs/nixos-25.11"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "'base' is required in " + path
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "It names the channel that builds the system (provides lib).") {
		t.Errorf("error %q missing hint", err.Error())
	}
}

func TestLoadChannels_NoChannelsEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `base = "stable"`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := path + " declares no [channels] entry"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestLoadChannels_InvalidName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "stable"

[channels]
"1stable" = "github:NixOS/nixpkgs/nixos-25.11"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "'1stable' in " + path + " is not a valid Nix identifier"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "Names must match ^[a-zA-Z_][a-zA-Z0-9_-]*$") {
		t.Errorf("error %q missing regex hint", err.Error())
	}
}

func TestLoadChannels_ReservedNameSelf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "self"

[channels]
self = "github:NixOS/nixpkgs/nixos-25.11"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "'self' in " + path + " is reserved — the generator emits it itself"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
}

func TestLoadChannels_ReservedNameNixpkgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "stable"

[channels]
stable = "github:NixOS/nixpkgs/nixos-25.11"
nixpkgs = "github:NixOS/nixpkgs/nixos-25.11"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "'nixpkgs' in " + path + " is reserved — the generator emits it itself"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
}

func TestLoadChannels_DuplicateNameAcrossSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "stable"

[channels]
stable = "github:NixOS/nixpkgs/nixos-25.11"

[flakes]
stable = "github:example/stable"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "name declared more than once in " + path + ":\n  - stable"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
}

func TestLoadChannels_BaseNotDeclaredChannel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "unstable"

[channels]
stable = "github:NixOS/nixpkgs/nixos-25.11"
`)

	_, err := LoadChannels(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "base = \"unstable\" is not a declared [channels] entry in " + path
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want contains %q", err.Error(), want)
	}
}

func TestLoadChannels_FlakesEntryValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.toml")
	writeFile(t, path, `
base = "stable"

[channels]
stable = "github:NixOS/nixpkgs/nixos-25.11"

[flakes]
home-manager = "github:nix-community/home-manager"
`)

	c, err := LoadChannels(path)
	if err != nil {
		t.Fatalf("LoadChannels: %v", err)
	}
	if len(c.Flakes) != 1 || c.Flakes[0].Name != "home-manager" {
		t.Fatalf("Flakes = %+v, want home-manager", c.Flakes)
	}
}
