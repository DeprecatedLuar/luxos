package shared

import (
	"errors"
	"fmt"

	"github.com/DeprecatedLuar/luxos/internal/password"
)

// ErrPasswordMismatch is returned when the two answers differ.
var ErrPasswordMismatch = errors.New("passwords do not match; nothing changed")

// AskNewPassword asks prompt, then again; both answers must match and not
// be empty. An empty first answer fails before the second question.
func AskNewPassword(ask func(prompt string) (string, error), prompt, again string) (string, error) {
	first, err := ask(prompt)
	if err != nil {
		return "", err
	}
	if first == "" {
		return "", fmt.Errorf("%w; nothing changed", password.ErrEmpty)
	}
	second, err := ask(again)
	if err != nil {
		return "", err
	}
	if first != second {
		return "", ErrPasswordMismatch
	}
	return first, nil
}
