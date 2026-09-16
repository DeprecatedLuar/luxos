package framework

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func embeddedModuleNames(t *testing.T) []string {
	t.Helper()
	srcFiles, _, err := embeddedModulesSet()
	if err != nil {
		t.Fatalf("embeddedModulesSet: %v", err)
	}
	names := make([]string, 0, len(srcFiles))
	for rel := range srcFiles {
		names = append(names, rel)
	}
	sort.Strings(names)
	return names
}

func TestSync_FreshDst(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "system")

	changes, err := Sync(dst)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	names := embeddedModuleNames(t)
	if len(changes) != len(names) {
		t.Fatalf("got %d changes, want %d (%v)", len(changes), len(names), changes)
	}
	for i, c := range changes {
		if c.Action != ActionCreated {
			t.Fatalf("change[%d] = %+v, want action %q", i, c, ActionCreated)
		}
	}

	for _, name := range names {
		info, err := os.Stat(filepath.Join(dst, name))
		if err != nil {
			t.Fatalf("Stat(%s): %v", name, err)
		}
		if info.Mode().Perm() != lockedMode {
			t.Fatalf("%s mode = %o, want %o", name, info.Mode().Perm(), lockedMode)
		}
	}
}

func TestSync_SecondRunNoChanges(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "system")

	if _, err := Sync(dst); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	changes, err := Sync(dst)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("second Sync changes = %v, want none", changes)
	}
}

func TestSync_ModifiedFileRestored(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "system")

	if _, err := Sync(dst); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	names := embeddedModuleNames(t)
	target := names[0]
	targetPath := filepath.Join(dst, target)
	wantData, err := files.ReadFile(filepath.Join(modulesSrcDir, target))
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(targetPath, writableMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("tampered"), writableMode); err != nil {
		t.Fatal(err)
	}

	changes, err := Sync(dst)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(changes) != 1 || changes[0].Path != target || changes[0].Action != ActionRestored {
		t.Fatalf("changes = %v, want single restored %q", changes, target)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(wantData) {
		t.Fatalf("content = %q, want %q", got, wantData)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != lockedMode {
		t.Fatalf("mode = %o, want %o", info.Mode().Perm(), lockedMode)
	}
}

func TestSync_ExtraFileAndDirRemoved(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "system")

	if _, err := Sync(dst); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	extraFile := filepath.Join(dst, "extra.nix")
	if err := os.WriteFile(extraFile, []byte("stray"), 0644); err != nil {
		t.Fatal(err)
	}
	extraDir := filepath.Join(dst, "extradir")
	if err := os.MkdirAll(extraDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extraDir, "nested.nix"), []byte("stray"), 0644); err != nil {
		t.Fatal(err)
	}

	changes, err := Sync(dst)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	gotPaths := map[string]string{}
	for _, c := range changes {
		gotPaths[c.Path] = c.Action
	}

	if action, ok := gotPaths["extra.nix"]; !ok || action != ActionRemoved {
		t.Fatalf("extra.nix change = %v, %v; want removed", ok, action)
	}
	if action, ok := gotPaths["extradir"]; !ok || action != ActionRemoved {
		t.Fatalf("extradir change = %v, %v; want removed", ok, action)
	}

	if _, err := os.Stat(extraFile); !os.IsNotExist(err) {
		t.Fatalf("extra.nix still exists: err=%v", err)
	}
	if _, err := os.Stat(extraDir); !os.IsNotExist(err) {
		t.Fatalf("extradir still exists: err=%v", err)
	}
}

func TestSync_DstSymlinkReplaced(t *testing.T) {
	tmp := t.TempDir()
	realOther := filepath.Join(tmp, "other")
	if err := os.MkdirAll(realOther, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(realOther, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(tmp, "system")
	if err := os.Symlink(realOther, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(dst); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("dst is still a symlink")
	}
	if !info.IsDir() {
		t.Fatalf("dst is not a directory")
	}

	// the old symlink target must be untouched
	data, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("symlink target was affected: %v", err)
	}
	if string(data) != "keep me" {
		t.Fatalf("sentinel content = %q, want %q", data, "keep me")
	}
}
