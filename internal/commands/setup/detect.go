// Package setup creates a luxos config on a computer that has none: it reads
// the running system, asks the user, and writes the config through
// internal/config.
package setup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

const (
	osReleaseFile = "etc/os-release"
	localtimeFile = "etc/localtime"
	localeFile    = "etc/locale.conf"
	keyboardFile  = "etc/X11/xorg.conf.d/00-keyboard.conf"

	versionIDKey    = "VERSION_ID="
	langKey         = "LANG="
	zoneinfoMarker  = "zoneinfo/"
	xkbLayoutOption = `Option "XkbLayout"`
	channelPrefix   = "nixos-"
	quoteChars      = `"'`
)

// System is what setup reads from the running system.
type System struct {
	Values  config.MachineValues
	Release string // the new machine's system.stateVersion
}

// Detect reads the running system under root. The release is required: a
// wrong stateVersion cannot be fixed later, so there is no fallback for it.
func Detect(root string) (System, error) {
	release, err := keyValue(filepath.Join(root, osReleaseFile), versionIDKey)
	if err != nil {
		return System{}, fmt.Errorf("read the NixOS release: %w", err)
	}
	if release == "" {
		return System{}, fmt.Errorf("read the NixOS release: no %s in %s", strings.TrimSuffix(versionIDKey, "="), filepath.Join(root, osReleaseFile))
	}

	v := config.MachineDefaults
	v.Channel = channelPrefix + release
	if tz := timezone(filepath.Join(root, localtimeFile)); tz != "" {
		v.Timezone = tz
	}
	if lang, err := keyValue(filepath.Join(root, localeFile), langKey); err == nil && lang != "" {
		v.Locale = lang
	}
	if layout := xkbLayout(filepath.Join(root, keyboardFile)); layout != "" {
		v.Keyboard = layout
	}
	return System{Values: v, Release: release}, nil
}

// keyValue returns the unquoted value of the first line of file starting with key.
func keyValue(file, key string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), key); ok {
			return strings.Trim(v, quoteChars), nil
		}
	}
	return "", sc.Err()
}

// timezone is the zone name in the /etc/localtime link target, or "".
func timezone(link string) string {
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	i := strings.LastIndex(target, zoneinfoMarker)
	if i < 0 {
		return ""
	}
	return target[i+len(zoneinfoMarker):]
}

// xkbLayout is the XkbLayout option value in an xorg keyboard file, or "".
func xkbLayout(file string) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), xkbLayoutOption); ok {
			return strings.Trim(strings.TrimSpace(v), quoteChars)
		}
	}
	return ""
}
