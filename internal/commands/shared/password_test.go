package shared

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/password"
)

// replies returns an ask that answers with each value in turn and records
// the prompts it was asked.
func replies(asked *[]string, values ...string) func(string) (string, error) {
	return func(prompt string) (string, error) {
		*asked = append(*asked, prompt)
		if len(values) == 0 {
			return "", io.EOF
		}
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

func TestAskNewPassword(t *testing.T) {
	var asked []string
	if pw, err := AskNewPassword(replies(&asked, "s3cret", "s3cret"), "P: ", "A: "); err != nil || pw != "s3cret" {
		t.Errorf("match = %q, %v", pw, err)
	}
	if strings.Join(asked, "|") != "P: |A: " {
		t.Errorf("prompts = %q", asked)
	}

	if _, err := AskNewPassword(replies(&asked, "a", "b"), "P: ", "A: "); !errors.Is(err, ErrPasswordMismatch) || !strings.HasSuffix(err.Error(), "; nothing changed") {
		t.Errorf("mismatch: %v", err)
	}

	asked = nil
	_, err := AskNewPassword(replies(&asked, "", "x"), "P: ", "A: ")
	if !errors.Is(err, password.ErrEmpty) || !strings.HasSuffix(err.Error(), "; nothing changed") {
		t.Errorf("empty: %v", err)
	}
	if len(asked) != 1 {
		t.Errorf("asked again after an empty answer: %q", asked)
	}

	if _, err := AskNewPassword(replies(&asked, "a"), "P: ", "A: "); !errors.Is(err, io.EOF) {
		t.Errorf("end of input: %v", err)
	}
}
