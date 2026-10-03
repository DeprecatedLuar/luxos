package commands

import (
	"errors"
	"fmt"
)

const unknownRevision = "unknown"

// revision is set by the flake build through -ldflags -X.
var revision = unknownRevision

func Version(args []string) error {
	if len(args) > 0 {
		return errors.New("version takes no arguments")
	}
	fmt.Println(revision)
	return nil
}
