package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// productUUIDRel is the sysfs path (relative to sysDir) of the board's UUID.
	productUUIDRel = "class/dmi/id/product_uuid"
	// keyPrefix salts the hash so the key is not the plain hash of the UUID.
	keyPrefix = "luxos-hardware:"
	// keyLen is the number of hex characters of the digest kept as the key.
	keyLen = 16
)

// HardwareKey returns the name of this computer's folder under the hardware
// root: the first keyLen hex characters of sha256(keyPrefix + product_uuid).
// The raw UUID is a permanent identifier and never leaves this function. An
// unreadable or empty UUID is an error; there is no fallback.
func HardwareKey(sysDir string) (string, error) {
	path := filepath.Join(sysDir, productUUIDRel)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: luxos needs it to pick this computer's hardware folder: %w", path, err)
	}
	uuid := strings.ToLower(strings.TrimSpace(string(data)))
	if uuid == "" {
		return "", fmt.Errorf("cannot read %s: luxos needs it to pick this computer's hardware folder: file is empty", path)
	}
	sum := sha256.Sum256([]byte(keyPrefix + uuid))
	return hex.EncodeToString(sum[:])[:keyLen], nil
}
