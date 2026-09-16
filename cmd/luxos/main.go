package main

import (
	"fmt"
	"os"
)

const usage = "luxos <command> [args]\n\nno commands yet"

func main() {
	if len(os.Args) < 2 || os.Args[1] == "help" {
		fmt.Println(usage)
		return
	}

	fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n", os.Args[1])
	os.Exit(1)
}
