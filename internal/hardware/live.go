package hardware

import "errors"

const (
	roStoreMount = "/nix/.ro-store"
	squashfs     = "squashfs"
)

// ErrLiveSystem is a system booted from an installer image instead of installed.
var ErrLiveSystem = errors.New("Errrm... It seems you are running from a live boot? I don't know how to handle it so I will just panic and do nothing. Thank you for the comprehension\n  Make sure to install real NixOS before running luxos")

// CheckInstalled returns ErrLiveSystem when the read-only store is a squashfs
// image, as on NixOS installer and netboot media. Other filesystems there
// (a build-vm guest uses 9p or erofs) do not count.
func CheckInstalled(mountsFile string) error {
	mounts, err := parseMounts(mountsFile)
	if err != nil {
		return err
	}
	if m, ok := mounts[roStoreMount]; ok && m.fstype == squashfs {
		return ErrLiveSystem
	}
	return nil
}
