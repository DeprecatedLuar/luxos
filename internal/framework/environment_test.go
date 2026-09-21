package framework

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const environmentEvalExpr = `(import ./framework/environment.nix {}).environment.sessionVariables`

type environmentCase struct {
	name    string
	file    string
	want    map[string]string
	errLine int // > 0: expect an error naming this line
}

func envOK(name, file string, want map[string]string) environmentCase {
	return environmentCase{name: name, file: file, want: want}
}

func envErr(name, file string, line int) environmentCase {
	return environmentCase{name: name, file: file, errLine: line}
}

func TestEnvironmentParser(t *testing.T) {
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
	tmpl, err := File("templates/environment")
	if err != nil {
		t.Fatal(err)
	}
	parser, err := File("environment.nix")
	if err != nil {
		t.Fatal(err)
	}

	cases := []environmentCase{
		envOK("1", "A=1", map[string]string{"A": "1"}),
		envOK("2", "export A=1", map[string]string{"A": "1"}),
		envOK("3", "  A=1", map[string]string{"A": "1"}),
		envOK("4", `A="x y"`, map[string]string{"A": "x y"}),
		envOK("5", `A='x y'`, map[string]string{"A": "x y"}),
		envOK("6", "A=", map[string]string{"A": ""}),
		envOK("7", "A=''", map[string]string{"A": ""}),
		envOK("8", "A=val # note", map[string]string{"A": "val"}),
		envOK("9", `A="v w" # note`, map[string]string{"A": "v w"}),
		envOK("10", "A=a#b", map[string]string{"A": "a#b"}),
		envOK("11", "# c\n\nA=1\n", map[string]string{"A": "1"}),
		envOK("12", "", map[string]string{}),
		envOK("13", "A='$5 a\\b c`d'", map[string]string{"A": "\\$5 a\\\\b c\\`d"}),
		envOK("14", "A=$HOME/x", map[string]string{"A": "$HOME/x"}),
		envOK("15", `A="${USER}-x"`, map[string]string{"A": "$USER-x"}),
		envOK("16", "A=$HOME/x\nB=$A/y", map[string]string{"A": "$HOME/x", "B": "$HOME/x/y"}),
		envOK("17", "A='$5'\nB=${A}0", map[string]string{"A": "\\$5", "B": "\\$50"}),
		envOK("18", `A="it's"`, map[string]string{"A": "it's"}),
		envOK("19", "A=me@x.org", map[string]string{"A": "me@x.org"}),
		envErr("20", "A = 1", 1),
		envErr("21", "1A=x", 1),
		envErr("22", "garbage", 1),
		envErr("23", "A=a b", 1),
		envErr("24", `A="a"b`, 1),
		envErr("25", "A=a'b'", 1),
		envErr("26", `A="open`, 1),
		envErr("27", `A=a\b`, 1),
		envErr("28", "A=\"a`b\"", 1),
		envErr("29", "A=$FOO", 1),
		envErr("30", "B=$A\nA=1", 1),
		envErr("31", "A=${HOME}x", 1),
		envErr("32", "A=$HOME\nB=${A}x", 2),
		envErr("33", "A=$", 1),
		envErr("34", `A='a"b'`, 1),
		envErr("35", "A='x@{y}'", 1),
		envErr("36", "A='$HOME'", 1),
		envErr("37", "A='x@'\nB=${A}{y}", 2),
		envErr("38", "PATH=/x", 1),
		envErr("39", "A=1\nA=2", 2),
		envErr("40", "# c\nHOME=/x", 2),
		envOK("template", string(tmpl), map[string]string{
			"XDG_CACHE_HOME":  "$HOME/.cache",
			"XDG_CONFIG_HOME": "$HOME/.config",
			"XDG_DATA_HOME":   "$HOME/.local/share",
			"XDG_STATE_HOME":  "$HOME/.local/state",
		}),
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range map[string][]byte{
				"framework/environment.nix": parser,
				"config/environment":        []byte(tc.file),
			} {
				path := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", environmentEvalExpr)
			cmd.Dir = dir
			var stdout, combined bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &combined
			runErr := cmd.Run()

			if tc.errLine > 0 {
				if runErr == nil {
					t.Fatalf("expected error, got %s", stdout.String())
				}
				want := "luxos environment: line " + strconv.Itoa(tc.errLine) + ":"
				if !strings.Contains(combined.String(), want) {
					t.Fatalf("output does not contain %q:\n%s", want, combined.String())
				}
				return
			}
			if runErr != nil {
				t.Fatalf("eval failed: %v\n%s", runErr, combined.String())
			}
			var got map[string]string
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", stdout.String(), err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
