package cli

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func removeEmptyCleanupDirectory(parent *os.Root, name string) error {
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	namePointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	// os.Root.Remove also unlinks files. Linux AT_REMOVEDIR makes this final
	// operation directory-only, so a late replacement file or link survives.
	const atRemovedir = 0x200
	_, _, errno := syscall.Syscall(syscall.SYS_UNLINKAT, directory.Fd(), uintptr(unsafe.Pointer(namePointer)), atRemovedir)
	runtime.KeepAlive(namePointer)
	runtime.KeepAlive(directory)
	if errno != 0 {
		return &os.PathError{Op: "unlinkat", Path: name, Err: errno}
	}
	return nil
}
