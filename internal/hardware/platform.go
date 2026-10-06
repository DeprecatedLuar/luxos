package hardware

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// platformProfileChoicesFile lists the platform profiles the firmware
// accepts, under sysfs.
const platformProfileChoicesFile = "firmware/acpi/platform_profile_choices"

// DetectPlatformProfiles returns the platform profiles the firmware offers,
// in the order the kernel lists them. A computer without a platform-profile
// driver has no such file and yields no profiles; any other read failure is
// an error naming the file.
func DetectPlatformProfiles(sysDir string) ([]string, error) {
	path := filepath.Join(sysDir, platformProfileChoicesFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return strings.Fields(string(data)), nil
}
