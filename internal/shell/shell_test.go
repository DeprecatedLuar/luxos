package shell

import (
	"os"
	"strings"
	"testing"
)

func TestOutputCapturesStderrIntoError(t *testing.T) {
	_, err := Output(Cmd{Bin: "sh", Args: []string{"-c", "echo boom >&2; exit 3"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want it to carry stderr", err)
	}
}

func TestOutputReturnsStdoutInDir(t *testing.T) {
	dir := t.TempDir()
	out, err := Output(Cmd{Bin: "pwd", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != dir {
		t.Errorf("pwd = %q, want %q", out, dir)
	}
}

func TestExitCode(t *testing.T) {
	code, ok := ExitCode(Run(Cmd{Bin: "sh", Args: []string{"-c", "exit 7"}}))
	if !ok || code != 7 {
		t.Errorf("ExitCode = %d, %v; want 7, true", code, ok)
	}
	if _, ok := ExitCode(nil); ok {
		t.Error("ExitCode(nil) reported an exit status")
	}
}

func TestRun_GivenStdin(t *testing.T) {
	err := Run(Cmd{Bin: "sh", Args: []string{"-c", `read x; test "$x" = ok`}, Stdin: strings.NewReader("ok\n")})
	if err != nil {
		t.Errorf("Run with Stdin: %v", err)
	}
}

func TestPrependBundledPath(t *testing.T) {
	old := bundledPath
	t.Cleanup(func() { bundledPath = old })
	cases := []struct {
		name, bundled, path, want string
		calls                     int
	}{
		{"empty is a no-op", "", "/usr/bin", "/usr/bin", 1},
		{"prepends", "/nix/store/x-git/bin", "/usr/bin", "/nix/store/x-git/bin:/usr/bin", 1},
		{"idempotent", "/nix/store/x-git/bin", "/usr/bin", "/nix/store/x-git/bin:/usr/bin", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bundledPath = c.bundled
			t.Setenv("PATH", c.path)
			for range c.calls {
				if err := PrependBundledPath(); err != nil {
					t.Fatal(err)
				}
			}
			if got := os.Getenv("PATH"); got != c.want {
				t.Errorf("PATH = %q, want %q", got, c.want)
			}
		})
	}
}
