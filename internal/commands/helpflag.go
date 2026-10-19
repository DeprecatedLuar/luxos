package commands

const (
	unknownFlagFormat = "unknown flag '%s'"
	unknownVerbFormat = "unknown verb %q"
)

func isHelpFlag(arg string) bool { return arg == "--help" || arg == "-h" }
