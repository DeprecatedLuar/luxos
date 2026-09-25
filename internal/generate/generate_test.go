package generate

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

var update = flag.Bool("update", false, "rewrite golden files")

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func skipIfNoBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
}

// assertGolden compares got against testdata/<name>.golden, rewriting the
// golden file instead when -update is passed.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// assertNixParses writes content to a temp .nix file and checks it parses
// with nix-instantiate --parse.
func assertNixParses(t *testing.T, content []byte) {
	t.Helper()
	skipIfNoNix(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "check.nix")
	if err := os.WriteFile(file, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := nix.Parse(file); err != nil {
		t.Errorf("nix-instantiate --parse failed: %v", err)
	}
}

func TestConfiguration_Golden(t *testing.T) {
	out, err := Configuration("paraloid")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	assertGolden(t, "configuration", out)
	assertNixParses(t, out)
}

func TestConfiguration_NoEtcLuxosBin(t *testing.T) {
	out, err := Configuration("paraloid")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	if strings.Contains(string(out), `environment.etc."luxos/bin"`) {
		t.Errorf("configuration.nix still declares environment.etc.\"luxos/bin\"")
	}
	if !strings.Contains(string(out), "inputs.luxos.packages") {
		t.Errorf("configuration.nix does not reference inputs.luxos.packages")
	}
}

func TestConfiguration_Deterministic(t *testing.T) {
	a, err := Configuration("paraloid")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	b, err := Configuration("paraloid")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("Configuration is not deterministic")
	}
}

func TestFlakeBootstrap_Golden(t *testing.T) {
	out, err := FlakeBootstrap("paraloid")
	if err != nil {
		t.Fatalf("FlakeBootstrap: %v", err)
	}
	assertGolden(t, "bootstrap", out)
	assertNixParses(t, out)
}

func TestFlakeBootstrap_Deterministic(t *testing.T) {
	a, err := FlakeBootstrap("paraloid")
	if err != nil {
		t.Fatalf("FlakeBootstrap: %v", err)
	}
	b, err := FlakeBootstrap("paraloid")
	if err != nil {
		t.Fatalf("FlakeBootstrap: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("FlakeBootstrap is not deterministic")
	}
}

func TestFlakeBootstrap_EmptyHostErrors(t *testing.T) {
	if _, err := FlakeBootstrap(""); err == nil {
		t.Fatalf("FlakeBootstrap: want error for empty host")
	}
}

func TestStaticFlakeNix_Parses(t *testing.T) {
	content, err := framework.File("flake.nix")
	if err != nil {
		t.Fatalf("framework.File(flake.nix): %v", err)
	}
	assertNixParses(t, content)
}

func TestSystemNix_Parses(t *testing.T) {
	content, err := framework.File("system.nix")
	if err != nil {
		t.Fatalf("framework.File(system.nix): %v", err)
	}
	assertNixParses(t, content)
}

func TestSystemNix_NoWriteShellScriptBin(t *testing.T) {
	content, err := framework.File("system.nix")
	if err != nil {
		t.Fatalf("framework.File(system.nix): %v", err)
	}
	if strings.Contains(string(content), "writeShellScriptBin") {
		t.Errorf("system.nix still references writeShellScriptBin; that belongs in configuration.nix now")
	}
}

func TestShadowScript_BashSyntaxValid(t *testing.T) {
	skipIfNoBash(t)

	body, err := framework.File("shadow.sh")
	if err != nil {
		t.Fatalf("framework.File(shadow.sh): %v", err)
	}

	script := "LUXOS_ARGS=(rebuild)\nREAL=/bin/true\n" + string(body)

	dir := t.TempDir()
	file := filepath.Join(dir, "shim.sh")
	if err := os.WriteFile(file, []byte(script), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd := exec.Command("bash", "-n", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("bash -n failed: %v\n%s", err, out)
	}
}
