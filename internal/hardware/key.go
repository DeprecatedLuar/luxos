package hardware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	productUUIDRel = "class/dmi/id/product_uuid"
	// Salts the hash so the key is not the plain hash of the UUID.
	keyPrefix = "luxos-hardware:"
	keyLen    = 16
)

var errEmptyUUID = errors.New("file is empty")

// The first keyLen hex characters of sha256(keyPrefix + product_uuid).
// The raw UUID is a permanent identifier and never leaves this function. An
// unreadable or empty UUID is an error; there is no fallback.
func Key(sysDir string) (string, error) {
	path := filepath.Join(sysDir, productUUIDRel)
	data, err := os.ReadFile(path)
	uuid := strings.ToLower(strings.TrimSpace(string(data)))
	if err == nil && uuid == "" {
		err = errEmptyUUID
	}
	if err != nil {
		return "", fmt.Errorf("cannot read %s: luxos needs it to pick this computer's hardware folder: %w", path, err)
	}
	sum := sha256.Sum256([]byte(keyPrefix + uuid))
	return hex.EncodeToString(sum[:])[:keyLen], nil
}
