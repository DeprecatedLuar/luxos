package hardware

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

func TestHardwareFacts_EvaluateAgainstOptions(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	decl, err := templates.File("framework/luxos-hardware.nix")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Render(Facts{DMI: DMI{Vendor: "V", Product: "P", ChassisType: 10}, PlatformProfiles: []string{"quiet", "balanced"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"luxos-hardware.nix": decl, "hardware-facts.nix": facts} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expr := `let lib = (import <nixpkgs> {}).lib; in (lib.evalModules { modules = [ ` +
		dir + `/luxos-hardware.nix ` + dir + `/hardware-facts.nix ]; }).config.luxos.hardware`
	out, err := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "--expr", expr).CombinedOutput()
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, out)
	}
	var got struct {
		Vendor      string `json:"vendor"`
		Product     string `json:"product"`
		ChassisType int    `json:"chassisType"`

		PlatformProfiles []string `json:"platformProfiles"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if got.Vendor != "V" || got.Product != "P" || got.ChassisType != 10 || len(got.PlatformProfiles) != 2 || got.PlatformProfiles[0] != "quiet" {
		t.Errorf("evaluated facts = %+v", got)
	}
}
