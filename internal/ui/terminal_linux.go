package ui

import (
	"syscall"
	"unsafe"
)

// isTerminalFD asks the kernel whether the descriptor supports terminal
// attributes. File mode alone is insufficient: /dev/null and several other
// character devices are not interactive terminals.
func isTerminalFD(fd uintptr) bool {
	var attributes syscall.Termios
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&attributes)),
		0,
		0,
		0,
	)
	return errno == 0
}
