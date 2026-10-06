package hardware

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// Facts is everything luxos detects about the computer for the build.
type Facts struct {
	GPUs             []GPU
	DMI              DMI
	PlatformProfiles []string
}

// Render renders f as framework/hardware-facts.nix, GPUs sorted by BusID
// for stable output.
func Render(f Facts) ([]byte, error) {
	sorted := make([]GPU, len(f.GPUs))
	copy(sorted, f.GPUs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].BusID < sorted[j].BusID })

	var b strings.Builder
	b.WriteString("{ ... }:\n{\n")
	fmt.Fprintf(&b, "  luxos.hardware.vendor = %s;\n", nix.String(f.DMI.Vendor))
	fmt.Fprintf(&b, "  luxos.hardware.product = %s;\n", nix.String(f.DMI.Product))
	fmt.Fprintf(&b, "  luxos.hardware.chassisType = %d;\n", f.DMI.ChassisType)
	b.WriteString("  luxos.hardware.platformProfiles = [ ")
	for _, p := range f.PlatformProfiles {
		b.WriteString(nix.String(p) + " ")
	}
	b.WriteString("];\n")
	if len(sorted) == 0 {
		b.WriteString("  luxos.hardware.gpus = [ ];\n")
	} else {
		b.WriteString("  luxos.hardware.gpus = [\n")
		for _, g := range sorted {
			fmt.Fprintf(&b, "    { busId = %q; vendor = %q; vendorId = %q; deviceId = %q; class = %q; bootVga = %t; }\n",
				g.BusID, g.Vendor, g.VendorID, g.DeviceID, g.Class, g.BootVGA)
		}
		b.WriteString("  ];\n")
	}
	b.WriteString("}\n")
	return []byte(b.String()), nil
}
