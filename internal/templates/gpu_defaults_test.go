package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// gpuFixture is one entry of a config.luxos.hardware.gpus fixture used by
// TestGPUDefaults, matching the luxos-hardware.nix submodule shape.
type gpuFixture struct {
	BusID    string
	Vendor   string
	VendorID string
	DeviceID string
	Class    string
	BootVGA  bool
}

func (g gpuFixture) nixLiteral() string {
	return fmt.Sprintf("{ busId = %q; vendor = %q; vendorId = %q; deviceId = %q; class = %q; bootVga = %t; }",
		g.BusID, g.Vendor, g.VendorID, g.DeviceID, g.Class, g.BootVGA)
}

func gpuListLiteral(gpus []gpuFixture) string {
	var b strings.Builder
	b.WriteString("[ ")
	for _, g := range gpus {
		b.WriteString(g.nixLiteral())
		b.WriteString(" ")
	}
	b.WriteString("]")
	return b.String()
}

// primeResult mirrors the JSON shape of the evaluated config.hardware.nvidia.prime
// attrset, using the stub option module's defaults ("" / false) as the
// "nothing set" baseline.
type primeResult struct {
	NvidiaBusId string `json:"nvidiaBusId"`
	IntelBusId  string `json:"intelBusId"`
	AmdgpuBusId string `json:"amdgpuBusId"`
	Offload     struct {
		Enable           bool `json:"enable"`
		EnableOffloadCmd bool `json:"enableOffloadCmd"`
	} `json:"offload"`
}

// gpuResult mirrors the JSON shape of every value the defaults file sets,
// using the stub options' defaults (empty lists, false, null) as the
// "nothing set" baseline.
type gpuResult struct {
	Prime           primeResult `json:"prime"`
	VideoDrivers    []string    `json:"videoDrivers"`
	Open            *bool       `json:"open"`
	Modesetting     bool        `json:"modesetting"`
	Package         string      `json:"package"`
	Warnings        []string    `json:"warnings"`
	ExtraPackages   []string    `json:"extraPackages"`
	ExtraPackages32 []string    `json:"extraPackages32"`
}

const stubDefaultPackage = "default"

func boolPtr(b bool) *bool { return &b }

// Device IDs taken from nvidia-generations.nix.
const (
	idTuring        = "0x1f97" // in no pre-Turing or legacy table
	idMaxwellVolta  = "0x1b80"
	idLegacy470     = "0x0fc6"
	idLegacy390     = "0x06c0"
	idUnknownNewest = "0x2fff" // in no table: a newer card
)

func nvidiaGPU(busID, deviceID string) gpuFixture {
	return gpuFixture{BusID: busID, Vendor: "nvidia", VendorID: "0x10de", DeviceID: deviceID, Class: "3d"}
}

// paraloidPair is a hybrid laptop layout: one integrated Intel GPU
// (boot_vga) and one NVIDIA GPU.
var paraloidPair = []gpuFixture{
	{BusID: "PCI:0@0:2:0", Vendor: "intel", VendorID: "0x8086", DeviceID: "0x9a49", Class: "vga", BootVGA: true},
	{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", DeviceID: "0x1f97", Class: "3d", BootVGA: false},
}

func TestGPUDefaults(t *testing.T) {
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}

	dir := t.TempDir()
	for _, name := range []string{"luxos-hardware.nix", "luxos-hardware-defaults.nix", "nvidia-generations.nix"} {
		content, err := File("framework/" + name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "framework", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	intelPackages := []string{"intel-media-driver", "intel-vaapi-driver"}
	intelPackages32 := []string{"i686-intel-media-driver", "i686-intel-vaapi-driver"}

	paraloidWant := gpuResult{
		VideoDrivers:    []string{"nvidia"},
		Open:            boolPtr(true),
		Modesetting:     true,
		Package:         stubDefaultPackage,
		ExtraPackages:   intelPackages,
		ExtraPackages32: intelPackages32,
	}
	paraloidWant.Prime.NvidiaBusId = "PCI:1@0:0:0"
	paraloidWant.Prime.IntelBusId = "PCI:0@0:2:0"
	paraloidWant.Prime.Offload.Enable = true
	paraloidWant.Prime.Offload.EnableOffloadCmd = true

	overrideWant := paraloidWant
	overrideWant.Prime.NvidiaBusId = "PCI:9@0:0:0"

	hostOpenWant := paraloidWant
	hostOpenWant.Open = boolPtr(false)

	nvidiaTuring := gpuResult{
		VideoDrivers: []string{"nvidia"},
		Open:         boolPtr(true),
		Modesetting:  true,
		Package:      stubDefaultPackage,
	}

	proprietary := func(pkg string) gpuResult {
		r := nvidiaTuring
		r.Open = boolPtr(false)
		r.Package = pkg
		return r
	}

	type tc struct {
		name         string
		gpus         []gpuFixture
		packages     []string // attributes of boot.kernelPackages.nvidiaPackages
		override     string   // extra config.* lines injected into the fixture module
		want         gpuResult
		warningsWant []string // substrings, one per expected warning, in order
	}

	cases := []tc{
		{name: "paraloid pair", gpus: paraloidPair, want: paraloidWant},
		{
			name: "nvidia only",
			gpus: []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idTuring)},
			want: nvidiaTuring,
		},
		{
			name: "two nvidia",
			gpus: []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idTuring), nvidiaGPU("PCI:2@0:0:0", idTuring)},
			want: nvidiaTuring,
		},
		{
			name: "intel+amd integrated ambiguous, neither boot_vga",
			gpus: []gpuFixture{
				{BusID: "PCI:0@0:2:0", Vendor: "intel", VendorID: "0x8086", DeviceID: "0x9a49", Class: "vga"},
				{BusID: "PCI:0@0:3:0", Vendor: "amd", VendorID: "0x1002", DeviceID: "0x1638", Class: "vga"},
				nvidiaGPU("PCI:1@0:0:0", idTuring),
			},
			want: gpuResult{
				VideoDrivers:    []string{"nvidia"},
				Open:            boolPtr(true),
				Modesetting:     true,
				Package:         stubDefaultPackage,
				ExtraPackages:   intelPackages,
				ExtraPackages32: intelPackages32,
			},
		},
		{
			name:     "user override wins",
			gpus:     paraloidPair,
			override: `config.hardware.nvidia.prime.nvidiaBusId = "PCI:9@0:0:0";`,
			want:     overrideWant,
		},
		{
			name:     "host open override wins",
			gpus:     paraloidPair,
			override: `config.hardware.nvidia.open = false;`,
			want:     hostOpenWant,
		},
		{
			name:     "maxwell-volta card gets legacy_580 when nixpkgs has it",
			gpus:     []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idMaxwellVolta)},
			packages: []string{"legacy_580"},
			want:     proprietary("legacy_580"),
		},
		{
			name: "maxwell-volta card keeps the default package when nixpkgs lacks legacy_580",
			gpus: []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idMaxwellVolta)},
			want: proprietary(stubDefaultPackage),
		},
		{
			name:     "turing plus maxwell-volta is proprietary",
			gpus:     []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idTuring), nvidiaGPU("PCI:2@0:0:0", idMaxwellVolta)},
			packages: []string{"legacy_580"},
			want:     proprietary("legacy_580"),
		},
		{
			name:         "legacy 470 card is not driven and warns with the package name",
			gpus:         []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idLegacy470)},
			packages:     []string{"legacy_470"},
			want:         gpuResult{Package: stubDefaultPackage},
			warningsWant: []string{"legacy_470"},
		},
		{
			name:         "legacy branch absent from nixpkgs warns there is no driver",
			gpus:         []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idLegacy390)},
			packages:     []string{"legacy_470"},
			want:         gpuResult{Package: stubDefaultPackage},
			warningsWant: []string{"no driver"},
		},
		{
			name: "nvidia device in no table is open",
			gpus: []gpuFixture{nvidiaGPU("PCI:1@0:0:0", idUnknownNewest)},
			want: nvidiaTuring,
		},
		{
			name: "amd only sets nothing",
			gpus: []gpuFixture{{BusID: "PCI:0@0:1:0", Vendor: "amd", VendorID: "0x1002", DeviceID: "0x1638", Class: "vga", BootVGA: true}},
			want: gpuResult{Package: stubDefaultPackage},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var packages strings.Builder
			for _, name := range c.packages {
				fmt.Fprintf(&packages, "config.boot.kernelPackages.nvidiaPackages.%s = %q;\n", name, name)
			}
			expr := fmt.Sprintf(`
let
  lib = (import <nixpkgs> {}).lib;
  primeStub = { lib, ... }: {
    options.hardware.nvidia.prime = {
      nvidiaBusId = lib.mkOption { type = lib.types.str; default = ""; };
      intelBusId = lib.mkOption { type = lib.types.str; default = ""; };
      amdgpuBusId = lib.mkOption { type = lib.types.str; default = ""; };
      offload = {
        enable = lib.mkOption { type = lib.types.bool; default = false; };
        enableOffloadCmd = lib.mkOption { type = lib.types.bool; default = false; };
      };
    };
  };
  systemdStub = { lib, ... }: {
    options.systemd.settings.Manager = lib.mkOption { type = lib.types.attrsOf lib.types.str; default = { }; };
  };
  driverStub = { lib, ... }: {
    options.services.xserver.videoDrivers = lib.mkOption { type = lib.types.listOf lib.types.str; default = [ ]; };
    options.hardware.nvidia.open = lib.mkOption { type = lib.types.nullOr lib.types.bool; default = null; };
    options.hardware.nvidia.modesetting.enable = lib.mkOption { type = lib.types.bool; default = false; };
    options.hardware.nvidia.package = lib.mkOption { type = lib.types.str; default = %q; };
    options.boot.kernelPackages.nvidiaPackages = lib.mkOption { type = lib.types.attrsOf lib.types.str; default = { }; };
    options.warnings = lib.mkOption { type = lib.types.listOf lib.types.str; default = [ ]; };
    options.hardware.graphics.extraPackages = lib.mkOption { type = lib.types.listOf lib.types.str; default = [ ]; };
    options.hardware.graphics.extraPackages32 = lib.mkOption { type = lib.types.listOf lib.types.str; default = [ ]; };
    config._module.args.pkgs = {
      intel-media-driver = "intel-media-driver";
      intel-vaapi-driver = "intel-vaapi-driver";
      pkgsi686Linux = {
        intel-media-driver = "i686-intel-media-driver";
        intel-vaapi-driver = "i686-intel-vaapi-driver";
      };
    };
  };
  fixture = { config, lib, ... }: {
    config.luxos.hardware.gpus = %s;
    %s
    %s
  };
  eval = lib.evalModules {
    modules = [
      ./framework/luxos-hardware.nix
      primeStub
      systemdStub
      driverStub
      ./framework/luxos-hardware-defaults.nix
      fixture
    ];
  };
  c = eval.config;
in {
  prime = c.hardware.nvidia.prime;
  videoDrivers = c.services.xserver.videoDrivers;
  open = c.hardware.nvidia.open;
  modesetting = c.hardware.nvidia.modesetting.enable;
  package = c.hardware.nvidia.package;
  warnings = c.warnings;
  extraPackages = c.hardware.graphics.extraPackages;
  extraPackages32 = c.hardware.graphics.extraPackages32;
}
`, stubDefaultPackage, gpuListLiteral(c.gpus), packages.String(), c.override)

			cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", expr)
			cmd.Dir = dir
			var stdout, combined bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &combined
			if err := cmd.Run(); err != nil {
				t.Fatalf("eval failed: %v\n%s", err, combined.String())
			}

			var got gpuResult
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", stdout.String(), err)
			}
			warnings := got.Warnings
			got.Warnings = nil
			if len(warnings) != len(c.warningsWant) {
				t.Fatalf("warnings = %q, want %d entries containing %q", warnings, len(c.warningsWant), c.warningsWant)
			}
			for i, sub := range c.warningsWant {
				if !strings.Contains(warnings[i], sub) {
					t.Errorf("warning %d = %q, want it to contain %q", i, warnings[i], sub)
				}
			}
			if !reflect.DeepEqual(got, normalize(c.want)) {
				t.Fatalf("got %+v, want %+v", got, normalize(c.want))
			}
		})
	}
}

// normalize turns the nil slices of an expected result into the empty lists
// JSON decoding yields.
func normalize(r gpuResult) gpuResult {
	if r.VideoDrivers == nil {
		r.VideoDrivers = []string{}
	}
	if r.ExtraPackages == nil {
		r.ExtraPackages = []string{}
	}
	if r.ExtraPackages32 == nil {
		r.ExtraPackages32 = []string{}
	}
	return r
}
