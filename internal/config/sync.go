package config

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	modulesSrcDir = "modules"

	lockedMode = 0444

	ActionCreated  = "created"
	ActionRestored = "restored"
	ActionRemoved  = "removed"
)

type SyncChange struct {
	Path   string // relative to dst
	Action string // ActionCreated | ActionRestored | ActionRemoved
}

// A missing or differing file is (re)written and locked to mode 0444; any
// file or directory under dst that is not part of the embedded set is
// removed. If dst is itself a symlink, the symlink is removed (not
// followed) and replaced with a real directory.
func Sync(dst string) ([]SyncChange, error) {
	modulesFS, err := templates.Dir(modulesSrcDir)
	if err != nil {
		return nil, err
	}
	srcFiles, dirSet, err := embeddedModulesSet(modulesFS)
	if err != nil {
		return nil, err
	}

	if info, err := os.Lstat(dst); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(dst); err != nil {
			return nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(dst, dirMode); err != nil {
		return nil, err
	}

	var changes []SyncChange

	for relPath := range srcFiles {
		dstPath := filepath.Join(dst, relPath)
		if err := os.MkdirAll(filepath.Dir(dstPath), dirMode); err != nil {
			return nil, err
		}

		wantData, err := fs.ReadFile(modulesFS, relPath)
		if err != nil {
			return nil, err
		}

		action, err := writeIfNeeded(dstPath, wantData)
		if err != nil {
			return nil, err
		}
		if action != "" {
			changes = append(changes, SyncChange{Path: relPath, Action: action})
		}
	}

	removed, err := removeExtras(dst, srcFiles, dirSet)
	if err != nil {
		return nil, err
	}
	changes = append(changes, removed...)

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })

	return changes, nil
}

// The set of every embedded file, by path relative to the modules root, and
// the set of directories that contain at least one (including the root, "").
func embeddedModulesSet(modulesFS fs.FS) (srcFiles, dirSet map[string]bool, err error) {
	srcFiles = map[string]bool{}
	dirSet = map[string]bool{"": true}

	err = fs.WalkDir(modulesFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		srcFiles[path] = true
		for dir := filepath.Dir(path); dir != "." && dir != "/"; dir = filepath.Dir(dir) {
			dirSet[dir] = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return srcFiles, dirSet, nil
}

// writeIfNeeded writes wantData to dstPath unless it already matches, and
// leaves the file at mode lockedMode either way. Returns the action taken, or
// "" if the content already matched.
func writeIfNeeded(dstPath string, wantData []byte) (string, error) {
	haveData, err := os.ReadFile(dstPath)
	action := ActionRestored
	switch {
	case os.IsNotExist(err):
		action = ActionCreated
	case err != nil:
		return "", err
	case bytes.Equal(haveData, wantData):
		return "", os.Chmod(dstPath, lockedMode)
	default:
		if err := os.Chmod(dstPath, fileMode); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(dstPath, wantData, fileMode); err != nil {
		return "", err
	}
	if err := os.Chmod(dstPath, lockedMode); err != nil {
		return "", err
	}
	return action, nil
}

// removeExtras removes every entry under dst that is neither an embedded file
// nor a directory holding one.
func removeExtras(dst string, srcFiles, dirSet map[string]bool) ([]SyncChange, error) {
	var changes []SyncChange
	if err := removeExtrasWalk(dst, "", srcFiles, dirSet, &changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func removeExtrasWalk(dst, rel string, srcFiles, dirSet map[string]bool, changes *[]SyncChange) error {
	fullPath := filepath.Join(dst, rel)

	if !dirSet[rel] && !srcFiles[rel] {
		if err := os.RemoveAll(fullPath); err != nil {
			return err
		}
		*changes = append(*changes, SyncChange{Path: rel, Action: ActionRemoved})
		return nil
	}

	info, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := removeExtrasWalk(dst, filepath.Join(rel, e.Name()), srcFiles, dirSet, changes); err != nil {
			return err
		}
	}
	return nil
}
