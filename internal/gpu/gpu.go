// Package gpu detects PCI display-class devices (VGA and 3D controllers)
// from sysfs and renders them as a static Nix fact file. It takes every
// directory as a parameter: nothing here resolves paths, prints, or exits -
// same shape as internal/boot.
package gpu

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sysfsPCIDevices is the sysfs directory (relative to sysDir) holding one
// entry per PCI device, keyed by its "<domain>:<bus>:<dev>.<fn>" address.
const sysfsPCIDevices = "bus/pci/devices"

// Per-device files read under each entry of sysfsPCIDevices.
const (
	classFile   = "class"
	vendorFile  = "vendor"
	deviceFile  = "device"
	bootVgaFile = "boot_vga"
)

// PCI-SIG display class codes (base class 0x03), as the top 16 bits of the
// 24-bit class code (class >> 8).
const (
	classCodeVGA = 0x0300
	class3D      = 0x0302
)

// classVGA and class3D are the values rendered in GPU.Class.
const (
	classVGA = "vga"
	class3d  = "3d"
)

// PCI vendor IDs, as read from the "vendor" sysfs file.
const (
	vendorIDIntel  = "0x8086"
	vendorIDAMD    = "0x1002"
	vendorIDNVIDIA = "0x10de"
)

// unknownVendor is the Vendor value for any vendor ID not in vendorNames.
const unknownVendor = "unknown"

// vendorNames maps a PCI vendor ID to the short name used in GPU.Vendor.
var vendorNames = map[string]string{
	vendorIDIntel:  "intel",
	vendorIDAMD:    "amd",
	vendorIDNVIDIA: "nvidia",
}

// addrPattern matches a PCI device directory name: "<domain>:<bus>:<dev>.<fn>",
// each field hex.
var addrPattern = regexp.MustCompile(`^([0-9a-fA-F]+):([0-9a-fA-F]+):([0-9a-fA-F]+)\.([0-9a-fA-F]+)$`)

// GPU is one detected PCI display-class device.
type GPU struct {
	BusID    string // "PCI:<bus>@<domain>:<dev>:<fn>", decimal - see G2.
	Vendor   string // "intel", "amd", "nvidia", or "unknown".
	VendorID string // raw hex vendor ID, e.g. "0x8086".
	DeviceID string // raw hex device ID; not rendered yet, kept for future use.
	Class    string // "vga" or "3d".
	BootVGA  bool
}

// readHexFile reads path, trims whitespace, and parses it as a hex integer
// (with or without a "0x" prefix).
func readHexFile(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse hex value %q from %s: %w", string(data), path, err)
	}
	return v, nil
}

// readTrimmed reads path and returns its trimmed content, or "" if it does
// not exist.
func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// busID renders addr (a PCI device directory name) as "PCI:<bus>@<domain>:<dev>:<fn>"
// per G2, all four fields converted from hex to decimal.
func busID(addr string) (string, error) {
	m := addrPattern.FindStringSubmatch(addr)
	if m == nil {
		return "", fmt.Errorf("device directory name %q does not match <domain>:<bus>:<dev>.<fn>", addr)
	}
	domain, err := strconv.ParseUint(m[1], 16, 64)
	if err != nil {
		return "", fmt.Errorf("parse domain from %q: %w", addr, err)
	}
	bus, err := strconv.ParseUint(m[2], 16, 64)
	if err != nil {
		return "", fmt.Errorf("parse bus from %q: %w", addr, err)
	}
	dev, err := strconv.ParseUint(m[3], 16, 64)
	if err != nil {
		return "", fmt.Errorf("parse device from %q: %w", addr, err)
	}
	fn, err := strconv.ParseUint(m[4], 16, 64)
	if err != nil {
		return "", fmt.Errorf("parse function from %q: %w", addr, err)
	}
	return fmt.Sprintf("PCI:%d@%d:%d:%d", bus, domain, dev, fn), nil
}

// Detect reads <sysDir>/bus/pci/devices and returns every PCI device whose
// class is VGA or 3D controller (G1). A missing devices directory returns
// (nil, nil) - an empty machine or container, not an error. Any other read
// error is a hard error naming the offending device directory.
func Detect(sysDir string) ([]GPU, error) {
	devicesDir := filepath.Join(sysDir, sysfsPCIDevices)

	entries, err := os.ReadDir(devicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", devicesDir, err)
	}

	var gpus []GPU
	for _, entry := range entries {
		addr := entry.Name()
		devDir := filepath.Join(devicesDir, addr)

		classPath := filepath.Join(devDir, classFile)
		if _, err := os.Stat(classPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", classPath, err)
		}

		class, err := readHexFile(classPath)
		if err != nil {
			return nil, fmt.Errorf("device %s: read class: %w", devDir, err)
		}

		classCode := class >> 8
		var className string
		switch classCode {
		case classCodeVGA:
			className = classVGA
		case class3D:
			className = class3d
		default:
			continue
		}

		vendorPath := filepath.Join(devDir, vendorFile)
		vendorRaw, err := readHexFile(vendorPath)
		if err != nil {
			return nil, fmt.Errorf("device %s: read vendor: %w", devDir, err)
		}
		vendorID := fmt.Sprintf("0x%04x", vendorRaw)
		vendorName, ok := vendorNames[vendorID]
		if !ok {
			vendorName = unknownVendor
		}

		deviceRaw, err := readTrimmed(filepath.Join(devDir, deviceFile))
		if err != nil {
			return nil, fmt.Errorf("device %s: read device: %w", devDir, err)
		}

		bootVgaRaw, err := readTrimmed(filepath.Join(devDir, bootVgaFile))
		if err != nil {
			return nil, fmt.Errorf("device %s: read boot_vga: %w", devDir, err)
		}

		id, err := busID(addr)
		if err != nil {
			return nil, fmt.Errorf("device %s: %w", devDir, err)
		}

		gpus = append(gpus, GPU{
			BusID:    id,
			Vendor:   vendorName,
			VendorID: vendorID,
			DeviceID: deviceRaw,
			Class:    className,
			BootVGA:  bootVgaRaw == "1",
		})
	}

	return gpus, nil
}

// Render produces a static, dependency-free Nix file setting
// luxos.hardware.gpus from gpus, sorted by BusID for stable output (G3).
func Render(gpus []GPU) ([]byte, error) {
	sorted := make([]GPU, len(gpus))
	copy(sorted, gpus)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].BusID < sorted[j].BusID })

	var b strings.Builder
	b.WriteString("{ ... }:\n{\n")
	if len(sorted) == 0 {
		b.WriteString("  luxos.hardware.gpus = [ ];\n")
	} else {
		b.WriteString("  luxos.hardware.gpus = [\n")
		for _, g := range sorted {
			fmt.Fprintf(&b, "    { busId = %q; vendor = %q; vendorId = %q; class = %q; bootVga = %t; }\n",
				g.BusID, g.Vendor, g.VendorID, g.Class, g.BootVGA)
		}
		b.WriteString("  ];\n")
	}
	b.WriteString("}\n")
	return []byte(b.String()), nil
}
