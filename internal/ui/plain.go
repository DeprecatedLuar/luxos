package ui

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	columnSep  = "\t"
	jsonIndent = "  "
)

// Plain writes one line per entry of lines, its columns tab-separated.
func Plain(w *strings.Builder, lines [][]string) {
	for _, cols := range lines {
		fmt.Fprintln(w, strings.Join(cols, columnSep))
	}
}

// JSON writes v indented by two spaces, then a newline.
func JSON(w *strings.Builder, v any) error {
	data, err := json.MarshalIndent(v, "", jsonIndent)
	if err != nil {
		return err
	}
	w.Write(data)
	w.WriteString("\n")
	return nil
}

// ErrJSONConflict is the error for --json given with another output flag.
func ErrJSONConflict(other string) error {
	return fmt.Errorf("--json cannot be combined with %s", other)
}
