//go:build darwin

package ui

import (
	"syscall"
	"unsafe"
)

// isTerminal reports whether fd refers to a terminal, via the TIOCGETA ioctl —
// the Darwin equivalent of Linux's TCGETS, and the same check
// golang.org/x/term performs on Darwin.
func isTerminal(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
