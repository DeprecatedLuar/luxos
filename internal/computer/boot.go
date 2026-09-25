// Package computer detects facts about the physical computer - boot loader, GPUs, hardware
// configuration - and writes or renders them as Nix. It takes every path as a parameter: nothing here
// resolves paths, prints, or exits.
package computer

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// efivarsRel is the sysfs path (relative to sysDir) whose presence means EFI.
const efivarsRel = "firmware/efi/efivars"

// classBlockRel is the sysfs directory (relative to sysDir) holding one
// entry per block device, keyed by kernel name.
const classBlockRel = "class/block"

// slavesDir is the subdirectory of a class/block entry listing the devices
// it is built on top of (LUKS, LVM, RAID). Non-empty means "not a plain
// disk or partition".
const slavesDir = "slaves"

// partitionFile, when present in a class/block entry, means that entry is a
// partition rather than a whole disk.
const partitionFile = "partition"

// vfat is the filesystem type an EFI system partition must be mounted as.
const vfat = "vfat"

// devPrefix is the prefix every BIOS boot device must have.
const devPrefix = "/dev/"

// fileMode is the mode boot.nix is written with.
const fileMode = 0644

// efiMounts are the mount points checked in order for a vfat EFI system
// partition.
var efiMounts = []string{"/boot/efi", "/boot"}

// biosMounts are the mount points checked in order for the BIOS boot
// device's source.
var biosMounts = []string{"/boot", "/"}

//go:embed templates/boot.nix.tmpl
var templatesFS embed.FS

var bootTmpl = template.Must(template.New("boot.nix.tmpl").ParseFS(templatesFS, "templates/boot.nix.tmpl"))

// Loader is the detected boot loader configuration for this computer.
// Target is the EFI mount point (B4) or the "/dev/<disk>" device (B5).
type Loader struct {
	EFI    bool
	Target string
}

// mountEntry is one parsed line of a mounts file.
type mountEntry struct {
	source string
	fstype string
}

// parseMounts reads a whitespace-separated mounts file (source mountpoint
// fstype ...). When a mount point appears on more than one line, the last
// line wins. Mount points are compared as raw strings.
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

// DetectBoot determines this computer's boot loader firmware and target from
// sysDir (a sysfs root, normally "/sys") and mountsFile (normally
// "/proc/mounts").
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

// detectEFI implements B4: the EFI target is the mount point of a vfat
// filesystem, /boot/efi taking priority over /boot.
func detectEFI(mounts map[string]mountEntry) (Loader, error) {
	for _, mp := range efiMounts {
		if entry, ok := mounts[mp]; ok && entry.fstype == vfat {
			return Loader{EFI: true, Target: mp}, nil
		}
	}
	return Loader{}, fmt.Errorf("no vfat filesystem mounted at %s", strings.Join(efiMounts, " or "))
}

// detectBIOS implements B5: the target is "/dev/<disk>" for the source
// device of the /boot mount, falling back to /.
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

// RenderBoot renders boot.nix for the detected loader (B10).
func RenderBoot(l Loader) ([]byte, error) {
	var buf bytes.Buffer
	if err := bootTmpl.Execute(&buf, l); err != nil {
		return nil, fmt.Errorf("computer.RenderBoot: %w", err)
	}
	return buf.Bytes(), nil
}

// EnsureBoot makes sure bootFile exists, writing it from detection when it does
// not. An existing entry of any kind is left alone (B1). A detection
// failure is a hard error naming bootFile (B7).
func EnsureBoot(bootFile, sysDir, mountsFile string) (created bool, err error) {
	if _, err := os.Lstat(bootFile); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("computer.EnsureBoot: stat %s: %w", bootFile, err)
	}

	loader, err := DetectBoot(sysDir, mountsFile)
	if err != nil {
		return false, fmt.Errorf("cannot detect the boot loader: %s\nwrite %s by hand to configure the boot loader", err, bootFile)
	}

	content, err := RenderBoot(loader)
	if err != nil {
		return false, err
	}

	if err := os.WriteFile(bootFile, content, fileMode); err != nil {
		return false, fmt.Errorf("computer.EnsureBoot: write %s: %w", bootFile, err)
	}

	return true, nil
}
