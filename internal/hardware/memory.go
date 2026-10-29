package hardware

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	meminfoFile = "meminfo"
	memTotalKey = "MemTotal:"
	kibibyte    = 1024
)

// DetectMemory returns the RAM size in bytes from <procDir>/meminfo. A missing
// file, a missing MemTotal line or a value that is not an integer is an error
// naming the file.
func DetectMemory(procDir string) (int64, error) {
	path := filepath.Join(procDir, meminfoFile)
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("cannot read %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != memTotalKey {
			continue
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: %s %q is not an integer", path, memTotalKey, fields[1])
		}
		return kb * kibibyte, nil
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return 0, fmt.Errorf("%s: no %s line", path, memTotalKey)
}
