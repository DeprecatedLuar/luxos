package hardware

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// cpuModaliasFile is the kernel's CPU modalias under sysfs, e.g.
// "cpu:type:x86,ven0000fam0006mod008C:feature:...".
const cpuModaliasFile = "devices/system/cpu/modalias"

// The kernel's x86 vendor number, four hex digits after "ven".
var cpuVendorPattern = regexp.MustCompile(`ven([0-9A-Fa-f]{4})`)

var cpuVendorNames = map[string]string{
	"0000": "intel",
	"0002": "amd",
}

// DetectCPUVendor returns "intel", "amd", or "unknown" when the modalias is
// missing, carries no vendor (non-x86) or names another vendor. Any other
// read failure is an error naming the file.
func DetectCPUVendor(sysDir string) (string, error) {
	path := filepath.Join(sysDir, cpuModaliasFile)
	modalias, err := readTrimmed(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	m := cpuVendorPattern.FindStringSubmatch(modalias)
	if m == nil {
		return unknownVendor, nil
	}
	if name, ok := cpuVendorNames[m[1]]; ok {
		return name, nil
	}
	return unknownVendor, nil
}
