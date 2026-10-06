package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// dmiDir holds the firmware's description of the computer under sysfs.
const dmiDir = "class/dmi/id"

const (
	sysVendorFile   = "sys_vendor"
	productNameFile = "product_name"
	chassisTypeFile = "chassis_type"
)

// DMI is what the firmware says the computer is, as the firmware wrote it.
// ChassisType is the SMBIOS chassis type integer.
type DMI struct {
	Vendor      string
	Product     string
	ChassisType int
}

// DetectDMI reads the vendor, product and chassis type from sysfs. A missing
// or unreadable file, or a chassis type that is not an integer, is an error
// naming the file.
func DetectDMI(sysDir string) (DMI, error) {
	vendor, err := readDMI(sysDir, sysVendorFile)
	if err != nil {
		return DMI{}, err
	}
	product, err := readDMI(sysDir, productNameFile)
	if err != nil {
		return DMI{}, err
	}
	raw, err := readDMI(sysDir, chassisTypeFile)
	if err != nil {
		return DMI{}, err
	}
	chassis, err := strconv.Atoi(raw)
	if err != nil {
		return DMI{}, fmt.Errorf("%s: chassis type %q is not an integer", filepath.Join(sysDir, dmiDir, chassisTypeFile), raw)
	}
	return DMI{Vendor: vendor, Product: product, ChassisType: chassis}, nil
}

func readDMI(sysDir, name string) (string, error) {
	path := filepath.Join(sysDir, dmiDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
