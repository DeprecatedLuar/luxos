package config

import (
	"os/exec"
	"strings"
	"testing"
)

// config takes its folders and its reporter as parameters; it never resolves
// paths or draws the terminal itself.
func TestConfigImportsNeitherPathsNorUI(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	for _, imp := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(imp, "/internal/paths") || strings.HasSuffix(imp, "/internal/ui") {
			t.Errorf("config imports %s", imp)
		}
	}
}
