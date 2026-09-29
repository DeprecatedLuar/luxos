package shared

import "fmt"

// Returned as an error so main can exit with it without printing anything
// (the child already did).
type ExitCode int

func (c ExitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }
