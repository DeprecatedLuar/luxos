package help

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	gohelp "github.com/DeprecatedLuar/gohelp-luar"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
)

const (
	briefText       = "usage: luxos <command> [args]\nrun 'luxos help' for commands\n"
	wantExitCode    = 2
	blockBarPrefix  = "──["
	moduleUsage     = "luxos module <verb> <name>"
	keyUsage        = "luxos user key <verb> <user>"
	causeText       = "want one user"
	unknownName     = "nope"
	topicHierarchy  = "hierarchy"
	commandSections = "rebuild module package flake user passwd setup key"
)

// userVerbSections are the command sections whose usage is a user verb.
var userVerbSections = map[string]bool{"passwd": true, "key": true}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(buf *bytes.Buffer) string { return ansi.ReplaceAllString(buf.String(), "") }

func assertExit2(t *testing.T, err error) {
	t.Helper()
	var code shared.ExitCode
	if !errors.As(err, &code) || int(code) != wantExitCode {
		t.Fatalf("err = %v, want exit code %d", err, wantExitCode)
	}
}

func TestSpecParses(t *testing.T) {
	if _, err := gohelp.Parse(spec); err != nil {
		t.Fatal(err)
	}
}

func TestCommandSectionsExist(t *testing.T) {
	for _, name := range strings.Fields(commandSections) {
		var buf bytes.Buffer
		if err := Command(&buf, name); err != nil {
			t.Fatalf("Command(%q) = %v", name, err)
		}
		want := "luxos " + name
		if userVerbSections[name] {
			want = "luxos user " + name
		}
		if !strings.Contains(buf.String(), want) {
			t.Errorf("block for %q lacks %q:\n%s", name, want, buf.String())
		}
	}
}

func TestCommandWritesBlock(t *testing.T) {
	var buf bytes.Buffer
	if err := Command(&buf, "module"); err != nil {
		t.Fatal(err)
	}
	out := plain(&buf)
	for _, want := range []string{moduleUsage, "enable <name>..."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "──[flake]") {
		t.Errorf("block leaks another section:\n%s", out)
	}
}

func TestCommandUnknown(t *testing.T) {
	var buf bytes.Buffer
	if err := Command(&buf, unknownName); err == nil {
		t.Fatal("Command on unknown name = nil, want error")
	}
}

func TestMisuse(t *testing.T) {
	var buf bytes.Buffer
	assertExit2(t, Misuse(&buf, "key", errors.New(causeText)))
	out := plain(&buf)
	cause, usage := strings.Index(out, causeText), strings.Index(out, keyUsage)
	if cause < 0 || usage < 0 || cause > usage {
		t.Fatalf("want cause before usage block:\n%s", out)
	}
}

func TestMisuseNoCause(t *testing.T) {
	var buf bytes.Buffer
	assertExit2(t, Misuse(&buf, "flake", nil))
	if !strings.HasPrefix(plain(&buf), blockBarPrefix) {
		t.Fatalf("want output to start with the block bar:\n%s", buf.String())
	}
}

func TestBrief(t *testing.T) {
	var buf bytes.Buffer
	assertExit2(t, Brief(&buf, nil))
	if buf.String() != briefText {
		t.Fatalf("got %q, want %q", buf.String(), briefText)
	}
}

func TestBriefWithCause(t *testing.T) {
	var buf bytes.Buffer
	assertExit2(t, Brief(&buf, errors.New(causeText)))
	out := plain(&buf)
	if !strings.Contains(out, causeText) || !strings.HasSuffix(out, briefText) {
		t.Fatalf("want cause then brief:\n%s", out)
	}
}

func TestRunUnknownTopic(t *testing.T) {
	if err := Run([]string{"help", unknownName}); err == nil {
		t.Fatal("Run on unknown topic = nil, want error")
	}
}

func TestRunTopic(t *testing.T) {
	if err := Run([]string{"help", topicHierarchy}); err != nil {
		t.Fatal(err)
	}
}

func TestRunCommandName(t *testing.T) {
	if err := Run([]string{"help", "module"}); err != nil {
		t.Fatal(err)
	}
}
