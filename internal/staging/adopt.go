package staging

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/DeprecatedLuar/luxos/internal/userfile"
)

// owned lists every entry in the staging root luxos generates and may
// delete. Everything not listed here and not in preserved is a stranger.
var owned = []string{
	"framework", "config", "flake.nix", "flake-file.nix",
	"configuration.nix", "flake.lock",
}

// preserved lists entries luxos never touches: facts about the computer,
// written once and then hand-edited.
var preserved = []string{"hardware-configuration.nix", "boot.nix"}

const (
	// adoptionMarker is the file whose presence means luxos already owns the
	// staging root, and luxosHeader the header its first line must carry.
	adoptionMarker = "framework/system.nix"
	luxosHeader    = "# LUXOS property - keep walking buddy"

	backupTimeFormat = "20060102T150405Z"

	changeMoved  = "moved"
	changeHealed = "healed"
)

// ErrNoBackupDir is returned by Adopt when something must be moved but no
// backup directory is known.
var ErrNoBackupDir = errors.New("no backup directory to move the existing /etc/nixos entries into")

// Change is one action Adopt took, for the caller to render.
type Change struct {
	Kind string // "moved" or "healed"
	Path string
	Dest string // destination for "moved", empty otherwise
}

// Adopt prepares stagingDir to be owned by luxos. It converts stagingDir
// from a symlink to a real directory, creates it when missing, and moves
// every entry luxos does not own into a fresh timestamped subdirectory of
// backupDir. On first adoption (adoptionMarker absent or not carrying
// luxosHeader) every entry except those in preserved is a stranger,
// because the files at owned paths are the user's real configuration.
// backupDir may be empty; it is only required when something must move,
// and Adopt returns ErrNoBackupDir in that case.
func Adopt(stagingDir, backupDir string) ([]Change, error) {
	var changes []Change

	info, err := os.Lstat(stagingDir)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		if err := os.Remove(stagingDir); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Kind: changeHealed, Path: stagingDir})
		if err := os.MkdirAll(stagingDir, dirMode); err != nil {
			return nil, err
		}
	case err == nil:
	case os.IsNotExist(err):
		if err := os.MkdirAll(stagingDir, dirMode); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	adopted, err := hasLuxosHeader(filepath.Join(stagingDir, adoptionMarker))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return nil, err
	}
	var strangers []string
	for _, e := range entries {
		name := e.Name()
		if slices.Contains(preserved, name) || (adopted && slices.Contains(owned, name)) {
			continue
		}
		strangers = append(strangers, name)
	}
	if len(strangers) == 0 {
		return changes, nil
	}
	if backupDir == "" {
		return nil, ErrNoBackupDir
	}

	if err := userfile.MkdirAll(backupDir); err != nil {
		return nil, err
	}
	leaf := filepath.Join(backupDir, time.Now().UTC().Format(backupTimeFormat))
	if err := userfile.Mkdir(leaf); err != nil {
		return nil, err
	}
	for _, name := range strangers {
		src := filepath.Join(stagingDir, name)
		dst := filepath.Join(leaf, name)
		if err := move(src, dst); err != nil {
			return nil, fmt.Errorf("move %s to %s: %w", src, dst, err)
		}
		if err := userfile.ChownTree(dst); err != nil {
			return nil, fmt.Errorf("chown %s: %w", dst, err)
		}
		changes = append(changes, Change{Kind: changeMoved, Path: src, Dest: dst})
	}
	return changes, nil
}

// Prune removes every owned entry from stagingDir. A missing entry is not an
// error.
func Prune(stagingDir string) error {
	for _, name := range owned {
		if err := os.RemoveAll(filepath.Join(stagingDir, name)); err != nil {
			return err
		}
	}
	return nil
}

// hasLuxosHeader reports whether path exists and its first line is luxosHeader.
func hasLuxosHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.TrimRight(line, "\r\n") == luxosHeader, nil
}

// move renames src to dst, falling back to copy-then-remove across
// filesystems.
func move(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// copyTree copies src to dst preserving symlinks as symlinks and file modes.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.Mkdir(target, info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyRegular(p, target, info.Mode().Perm())
		default:
			return fmt.Errorf("cannot copy %s: unsupported file type", p)
		}
	})
}

func copyRegular(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
