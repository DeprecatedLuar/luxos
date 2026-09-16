package shared

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Confirm prints prompt and reads one line of input from stdin, returning
// true for "y"/"Y" and false for anything else, including no input at all
// (an empty line or EOF). defYes is currently unused by any caller (every
// port so far reads bash's [[ "$reply" =~ ^[Yy]$ ]], which always defaults
// to "no"); it is kept so a future prompt can opt into a default-yes
// reading without changing this signature.
func Confirm(prompt string, defYes bool) (bool, error) {
	fmt.Print(prompt)

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}

	reply := strings.TrimSpace(line)
	if reply == "" {
		return defYes, nil
	}
	return reply == "y" || reply == "Y", nil
}
