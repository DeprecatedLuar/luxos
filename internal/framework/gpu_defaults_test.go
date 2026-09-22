package framework

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
	Class    string
	BootVGA  bool
}

func (g gpuFixture) nixLiteral() string {
	return fmt.Sprintf("{ busId = %q; vendor = %q; vendorId = %q; class = %q; bootVga = %t; }",
		g.BusID, g.Vendor, g.VendorID, g.Class, g.BootVGA)
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

func nothingSet() primeResult { return primeResult{} }

// paraloidPair is the laptop layout from implementation-plan.md 6b: one
// integrated Intel GPU (boot_vga) and one NVIDIA GPU.
var paraloidPair = []gpuFixture{
	{BusID: "PCI:0@0:2:0", Vendor: "intel", VendorID: "0x8086", Class: "vga", BootVGA: true},
	{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
}

func TestGPUDefaults(t *testing.T) {
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}

	hardwareOpt, err := File("luxos-hardware.nix")
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := File("luxos-hardware-defaults.nix")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for rel, content := range map[string][]byte{
		"framework/luxos-hardware.nix":          hardwareOpt,
		"framework/luxos-hardware-defaults.nix": defaults,
	} {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	type tc struct {
		name     string
		gpus     []gpuFixture
		override string // extra config.* lines injected into the fixture module
		want     primeResult
	}

	paraloidWant := primeResult{
		NvidiaBusId: "PCI:1@0:0:0",
		IntelBusId:  "PCI:0@0:2:0",
	}
	paraloidWant.Offload.Enable = true
	paraloidWant.Offload.EnableOffloadCmd = true

	overrideWant := paraloidWant
	overrideWant.NvidiaBusId = "PCI:9@0:0:0"

	cases := []tc{
		{
			name: "paraloid pair",
			gpus: paraloidPair,
			want: paraloidWant,
		},
		{
			name: "nvidia only",
			gpus: []gpuFixture{
				{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
			},
			want: nothingSet(),
		},
		{
			name: "two nvidia",
			gpus: []gpuFixture{
				{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
				{BusID: "PCI:2@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
			},
			want: nothingSet(),
		},
		{
			name: "intel+amd integrated ambiguous, neither boot_vga",
			gpus: []gpuFixture{
				{BusID: "PCI:0@0:2:0", Vendor: "intel", VendorID: "0x8086", Class: "vga", BootVGA: false},
				{BusID: "PCI:0@0:3:0", Vendor: "amd", VendorID: "0x1002", Class: "vga", BootVGA: false},
				{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
			},
			want: nothingSet(),
		},
		{
			name:     "user override wins",
			gpus:     paraloidPair,
			override: `config.hardware.nvidia.prime.nvidiaBusId = "PCI:9@0:0:0";`,
			want:     overrideWant,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
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
  fixture = { config, lib, ... }: {
    config.luxos.hardware.gpus = %s;
    %s
  };
  eval = lib.evalModules {
    modules = [
      ./framework/luxos-hardware.nix
      primeStub
      ./framework/luxos-hardware-defaults.nix
      fixture
    ];
  };
in eval.config.hardware.nvidia.prime
`, gpuListLiteral(c.gpus), c.override)

			cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", expr)
			cmd.Dir = dir
			var stdout, combined bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &combined
			if err := cmd.Run(); err != nil {
				t.Fatalf("eval failed: %v\n%s", err, combined.String())
			}

			var got primeResult
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", stdout.String(), err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
