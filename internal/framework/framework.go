package framework

import "embed"

//go:embed all:files
var files embed.FS

func File(name string) ([]byte, error) {
	return files.ReadFile("files/" + name)
}
