package swap

import (
	"os"
	"path/filepath"
	"testing"
)

const swapsHeader = "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"

func TestActive(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		want    bool
	}{
		"header only": {swapsHeader, false},
		"one device":  {swapsHeader + "/dev/zram0  partition\t5963772\t0\t5\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			procDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(procDir, "swaps"), []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := Active(procDir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestActive_Missing(t *testing.T) {
	if _, err := Active(t.TempDir()); err == nil {
		t.Error("want an error")
	}
}
