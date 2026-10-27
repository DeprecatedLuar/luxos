package hardware

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCheckInstalled(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		live  bool
	}{
		{"installer", []string{"/dev/loop0 /nix/.ro-store squashfs ro 0 0"}, true},
		{"build-vm 9p", []string{"nix-store /nix/.ro-store 9p rw 0 0"}, false},
		{"build-vm erofs", []string{"/dev/vdb /nix/.ro-store erofs ro 0 0"}, false},
		{"installed", []string{"/dev/sda2 / ext4 rw 0 0"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckInstalled(writeMounts(t, t.TempDir(), c.lines...))
			if c.live && !errors.Is(err, ErrLiveSystem) {
				t.Errorf("err = %v, want ErrLiveSystem", err)
			}
			if !c.live && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}

func TestCheckInstalled_MissingFile(t *testing.T) {
	err := CheckInstalled(filepath.Join(t.TempDir(), "none"))
	if err == nil || errors.Is(err, ErrLiveSystem) {
		t.Errorf("err = %v, want a read error", err)
	}
}

func TestErrLiveSystemText(t *testing.T) {
	want := "Errrm... It seems you are running from a live boot? I don't know how to handle it so I will just panic and do nothing. Thank you for the comprehension\n  Make sure to install real NixOS before running luxos"
	if ErrLiveSystem.Error() != want {
		t.Errorf("text = %q", ErrLiveSystem.Error())
	}
}
