package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHardwareSelected(t *testing.T) {
	cases := map[string]struct {
		body string
		want bool
	}{
		"present":   {"{ ... }:\n{\n  imports = [\n    ./local/hardware-support\n  ];\n}\n", true},
		"commented": {"{ ... }:\n{\n  imports = [\n    # ./local/hardware-support\n  ];\n}\n", false},
		"absent":    {"{ ... }:\n{\n  imports = [\n    ./system/gaming.nix\n  ];\n}\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, modulesFileName), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := hardwareSelected(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
