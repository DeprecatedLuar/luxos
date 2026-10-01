package shell

import (
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
