package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// IsTTY reports whether f is a terminal device, asked of the driver (IsTerminal
// accepts any character device, /dev/null included).
func IsTTY(f *os.File) (bool, error) {
	_, err := termios(f)
	if errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.EINVAL) {
		return false, nil
	}
	return err == nil, err
}

// AskSecret prints prompt to w and reads one line from in, without echo when
// in is a terminal. Input that ends with nothing typed returns io.EOF.
func AskSecret(in *os.File, w io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(w, prompt); err != nil {
		return "", err
	}
	term, err := IsTTY(in)
	if err != nil {
		return "", err
	}
	if term {
		old, err := termios(in)
		if err != nil {
			return "", err
		}
		quiet := old
		quiet.Lflag &^= syscall.ECHO
		if err := setTermios(in, &quiet); err != nil {
			return "", err
		}
		defer func() {
			_ = setTermios(in, &old)
			fmt.Fprintln(w)
		}()
	}
	return readLine(in)
}

// readLine reads byte by byte so a following read on the same pipe gets the
// next line. Only the trailing newline is removed.
func readLine(in io.Reader) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := in.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return string(line), nil
			}
			line = append(line, b[0])
		}
		if err == io.EOF {
			if len(line) == 0 {
				return "", io.EOF
			}
			return string(line), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func termios(f *os.File) (syscall.Termios, error) {
	var t syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t))); errno != 0 {
		return t, errno
	}
	return t, nil
}

func setTermios(f *os.File, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
