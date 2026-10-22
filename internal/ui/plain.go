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

// KV writes one key=value line per pair, in order.
func KV(w *strings.Builder, pairs [][2]string) {
	for _, kv := range pairs {
		fmt.Fprintf(w, "%s=%s\n", kv[0], kv[1])
	}
}

// JSONOneOrMany writes the one element of objs as an object, or objs as an
// array when it holds any other number.
func JSONOneOrMany[T any](w *strings.Builder, objs []T) error {
	if len(objs) == 1 {
		return JSON(w, objs[0])
	}
	return JSON(w, objs)
}
