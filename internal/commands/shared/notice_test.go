package shared

import (
	"bytes"
	"testing"
)

func TestKeyNoticePlain(t *testing.T) {
	var b bytes.Buffer
	KeyNotice(&b, "luar", "AGE-SECRET-KEY-1X")
	want := "\nSave this random key somewhere safe, you will need it on every computer that imports luar:\n\nAGE-SECRET-KEY-1X\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
