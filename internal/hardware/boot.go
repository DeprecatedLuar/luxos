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

// Target is the EFI mount point (B4) or the "/dev/<disk>" device (B5).
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

func DetectBoot(sysDir, mountsFile string) (Loader, error) {
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

	return detectBIOS(sysDir, mounts)
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

// The target is "/dev/<disk>" for the source device of the /boot mount, falling back to /.
func detectBIOS(sysDir string, mounts map[string]mountEntry) (Loader, error) {
	var device string
	for _, mp := range biosMounts {
		if entry, ok := mounts[mp]; ok {
			device = entry.source
			break
		}
	}
	if device == "" {
		return Loader{}, fmt.Errorf("no mount found for %s", strings.Join(biosMounts, " or "))
	}
	if !strings.HasPrefix(device, devPrefix) {
		return Loader{}, fmt.Errorf("device %q is not a block device under %s", device, devPrefix)
	}

	name := strings.TrimPrefix(device, devPrefix)
	classPath := filepath.Join(sysDir, classBlockRel, name)
	if _, err := os.Lstat(classPath); err != nil {
		return Loader{}, fmt.Errorf("device %s missing under %s", name, filepath.Join(sysDir, classBlockRel))
	}

	slavesPath := filepath.Join(classPath, slavesDir)
	if entries, err := os.ReadDir(slavesPath); err == nil && len(entries) > 0 {
		return Loader{}, fmt.Errorf("device %s has slaves (LUKS, LVM or RAID)", name)
	}

	disk := name
	if _, err := os.Stat(filepath.Join(classPath, partitionFile)); err == nil {
		resolved, err := filepath.EvalSymlinks(classPath)
		if err != nil {
			return Loader{}, fmt.Errorf("resolve %s: %w", classPath, err)
		}
		disk = filepath.Base(filepath.Dir(resolved))
	}

	return Loader{EFI: false, Target: devPrefix + disk}, nil
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
