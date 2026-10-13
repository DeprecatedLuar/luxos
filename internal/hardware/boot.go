// Package hardware reads facts about the physical computer from /sys and
// /proc (its key, boot loader and GPUs) and renders them as Nix.
package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const efivarsRel = "firmware/efi/efivars"
const classBlockRel = "class/block"
const byIDRel = "disk/by-id"

// Non-empty means "not a plain disk or partition".
const slavesDir = "slaves"

const partitionFile = "partition"
const vfat = "vfat"
const devPrefix = "/dev/"

// nixpkgs defaults to 100, which no small EFI system partition can
// hold - at roughly 39M per distinct kernel+initrd pair a 196M partition
// fits four. Installing the loader is the only thing that prunes /boot;
// nix.gc never touches it.
const configurationLimit = 4

var efiMounts = []string{"/boot/efi", "/boot"}
var biosMounts = []string{"/boot", "/"}

// Loader is the detected GRUB target: the EFI mount point when EFI, else the
// BIOS boot disk as "/dev/disk/by-id/<id>" when one exists, or "/dev/<disk>".
type Loader struct {
	EFI    bool
	Target string
}

type mountEntry struct {
	source string
	fstype string
}

// When a mount point appears on more than one line, the last line wins.
// Mount points are compared as raw strings.
func parseMounts(mountsFile string) (map[string]mountEntry, error) {
	data, err := os.ReadFile(mountsFile)
	if err != nil {
		return nil, fmt.Errorf("read mounts file %s: %w", mountsFile, err)
	}

	mounts := make(map[string]mountEntry)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mounts[fields[1]] = mountEntry{source: fields[0], fstype: fields[2]}
	}
	return mounts, nil
}

func DetectBoot(sysDir, devDir, mountsFile string) (Loader, error) {
	mounts, err := parseMounts(mountsFile)
	if err != nil {
		return Loader{}, err
	}

	efivarsPath := filepath.Join(sysDir, efivarsRel)
	if _, err := os.Lstat(efivarsPath); err == nil {
		return detectEFI(mounts)
	} else if !os.IsNotExist(err) {
		return Loader{}, fmt.Errorf("stat %s: %w", efivarsPath, err)
	}

	return detectBIOS(sysDir, devDir, mounts)
}

// /boot/efi takes priority over /boot.
func detectEFI(mounts map[string]mountEntry) (Loader, error) {
	for _, mp := range efiMounts {
		if entry, ok := mounts[mp]; ok && entry.fstype == vfat {
			return Loader{EFI: true, Target: mp}, nil
		}
	}
	return Loader{}, fmt.Errorf("no vfat filesystem mounted at %s", strings.Join(efiMounts, " or "))
}

// The target is the disk holding the source device of the /boot mount,
// falling back to /.
func detectBIOS(sysDir, devDir string, mounts map[string]mountEntry) (Loader, error) {
	source, err := biosSource(mounts)
	if err != nil {
		return Loader{}, err
	}
	name, err := resolveDevice(devDir, source)
	if err != nil {
		return Loader{}, err
	}
	disk, err := parentDisk(sysDir, name)
	if err != nil {
		return Loader{}, err
	}
	target, err := stableDiskPath(devDir, disk)
	if err != nil {
		return Loader{}, err
	}
	return Loader{EFI: false, Target: target}, nil
}

func biosSource(mounts map[string]mountEntry) (string, error) {
	for _, mp := range biosMounts {
		if entry, ok := mounts[mp]; ok {
			if !strings.HasPrefix(entry.source, devPrefix) {
				return "", fmt.Errorf("device %q is not a block device under %s", entry.source, devPrefix)
			}
			return entry.source, nil
		}
	}
	return "", fmt.Errorf("no mount found for %s", strings.Join(biosMounts, " or "))
}

// resolveDevice follows udev symlinks (e.g. /dev/disk/by-uuid/<id>) to the
// kernel device name (e.g. "vda1") under devDir.
func resolveDevice(devDir, source string) (string, error) {
	root, err := filepath.EvalSymlinks(devDir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", devDir, err)
	}
	path := filepath.Join(devDir, strings.TrimPrefix(source, devPrefix))
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve device %s: %w", source, err)
	}
	name, err := filepath.Rel(root, resolved)
	if err != nil || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("device %s resolves to %s, outside %s", source, resolved, devDir)
	}
	return name, nil
}

// parentDisk maps a kernel device name to its whole disk via sysfs: a
// partition yields its parent, a plain disk yields itself.
func parentDisk(sysDir, name string) (string, error) {
	classPath := filepath.Join(sysDir, classBlockRel, name)
	if _, err := os.Lstat(classPath); err != nil {
		return "", fmt.Errorf("device %s missing under %s", name, filepath.Join(sysDir, classBlockRel))
	}

	slavesPath := filepath.Join(classPath, slavesDir)
	if entries, err := os.ReadDir(slavesPath); err == nil && len(entries) > 0 {
		return "", fmt.Errorf("device %s has slaves (LUKS, LVM or RAID)", name)
	}

	if _, err := os.Stat(filepath.Join(classPath, partitionFile)); err != nil {
		return name, nil
	}
	resolved, err := filepath.EvalSymlinks(classPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", classPath, err)
	}
	return filepath.Base(filepath.Dir(resolved)), nil
}

// stableDiskPath returns "/dev/disk/by-id/<id>" for the first (sorted) by-id
// link resolving to disk, since kernel names can change between boots.
// Without one it returns "/dev/<disk>".
func stableDiskPath(devDir, disk string) (string, error) {
	fallback := devPrefix + disk
	diskPath, err := filepath.EvalSymlinks(filepath.Join(devDir, disk))
	if err != nil {
		return "", fmt.Errorf("resolve disk %s: %w", disk, err)
	}

	byIDDir := filepath.Join(devDir, byIDRel)
	entries, err := os.ReadDir(byIDDir)
	if os.IsNotExist(err) {
		return fallback, nil
	} else if err != nil {
		return "", fmt.Errorf("read %s: %w", byIDDir, err)
	}

	for _, e := range entries {
		link := filepath.Join(byIDDir, e.Name())
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", link, err)
		}
		if resolved == diskPath {
			return devPrefix + byIDRel + "/" + e.Name(), nil
		}
	}
	return fallback, nil
}

func RenderBoot(l Loader) ([]byte, error) {
	data := struct {
		Loader
		ConfigurationLimit int
	}{l, configurationLimit}
	out, err := templates.Render("boot.nix.tmpl", data)
	if err != nil {
		return nil, fmt.Errorf("hardware.RenderBoot: %w", err)
	}
	return out, nil
}
