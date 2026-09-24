//go:build linux

package ui

import (
	"syscall"
	"unsafe"
)

// isTerminal reports whether fd refers to a terminal, via the TCGETS ioctl —
// the same check golang.org/x/term performs on Linux.
func isTerminal(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
