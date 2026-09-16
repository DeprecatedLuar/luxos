package framework

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	modulesSrcDir = "files/modules"

	dirMode      = 0755
	writableMode = 0644
	lockedMode   = 0444

	ActionCreated  = "created"
	ActionRestored = "restored"
	ActionRemoved  = "removed"
)

//go:embed all:files
var files embed.FS

func File(name string) ([]byte, error) {
	return files.ReadFile("files/" + name)
}

// Change describes one filesystem action Sync took under dst.
type Change struct {
	Path   string // relative to dst
	Action string // ActionCreated | ActionRestored | ActionRemoved
}

// Sync makes dst byte-for-byte match the embedded files/modules tree: a
// missing or differing file is (re)written and locked to mode 0444; any
// file or directory under dst that is not part of the embedded set is
// removed. If dst is itself a symlink, the symlink is removed (not
// followed) and replaced with a real directory.
func Sync(dst string) ([]Change, error) {
	srcFiles, dirSet, err := embeddedModulesSet()
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

	var changes []Change

	for relPath, srcPath := range srcFiles {
		dstPath := filepath.Join(dst, relPath)
		if err := os.MkdirAll(filepath.Dir(dstPath), dirMode); err != nil {
			return nil, err
		}

		wantData, err := files.ReadFile(srcPath)
		if err != nil {
			return nil, err
		}

		action, err := writeIfNeeded(dstPath, wantData)
		if err != nil {
			return nil, err
		}
		if action != "" {
			changes = append(changes, Change{Path: relPath, Action: action})
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

// embeddedModulesSet returns, keyed by path relative to files/modules: the
// embedded FS path for every file, and the set of directories that contain
// at least one embedded file (including the root, "").
func embeddedModulesSet() (srcFiles map[string]string, dirSet map[string]bool, err error) {
	srcFiles = map[string]string{}
	dirSet = map[string]bool{}

	err = fs.WalkDir(files, modulesSrcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(modulesSrcDir, path)
		if err != nil {
			return err
		}
		srcFiles[rel] = path

		for dir := filepath.Dir(rel); dir != "." && dir != "/"; dir = filepath.Dir(dir) {
			dirSet[dir] = true
		}
		dirSet[""] = true

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return srcFiles, dirSet, nil
}

// writeIfNeeded writes wantData to dstPath when missing or differing, and
// always leaves the file at mode lockedMode. It returns the action taken,
// or "" if the file already matched.
func writeIfNeeded(dstPath string, wantData []byte) (string, error) {
	haveData, err := os.ReadFile(dstPath)
	switch {
	case os.IsNotExist(err):
		if err := os.WriteFile(dstPath, wantData, writableMode); err != nil {
			return "", err
		}
		if err := os.Chmod(dstPath, lockedMode); err != nil {
			return "", err
		}
		return ActionCreated, nil
	case err != nil:
		return "", err
	case bytes.Equal(haveData, wantData):
		return "", nil
	default:
		if err := os.Chmod(dstPath, writableMode); err != nil {
			return "", err
		}
		if err := os.WriteFile(dstPath, wantData, writableMode); err != nil {
			return "", err
		}
		if err := os.Chmod(dstPath, lockedMode); err != nil {
			return "", err
		}
		return ActionRestored, nil
	}
}

// removeExtras deletes every file or directory under dst that is not part
// of the embedded source set.
func removeExtras(dst string, srcFiles map[string]string, dirSet map[string]bool) ([]Change, error) {
	var changes []Change

	entries, err := os.ReadDir(dst)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		rel := e.Name()
		if err := removeExtrasWalk(dst, rel, srcFiles, dirSet, &changes); err != nil {
			return nil, err
		}
	}

	return changes, nil
}

func removeExtrasWalk(dst, rel string, srcFiles map[string]string, dirSet map[string]bool, changes *[]Change) error {
	fullPath := filepath.Join(dst, rel)

	if !dirIsKnown(rel, dirSet) && !fileIsKnown(rel, srcFiles) {
		if err := os.RemoveAll(fullPath); err != nil {
			return err
		}
		*changes = append(*changes, Change{Path: rel, Action: ActionRemoved})
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

func fileIsKnown(rel string, srcFiles map[string]string) bool {
	_, ok := srcFiles[rel]
	return ok
}

func dirIsKnown(rel string, dirSet map[string]bool) bool {
	return dirSet[rel]
}
