package storage

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrMutationBusy means a mutator or runtime admission owns the data inode.
var ErrMutationBusy = errors.New("another data mutation or runtime startup is active")

// MutationLock excludes installers, runtime admission and destructive data
// cleanup for one existing directory. It creates neither root nor lock file.
type MutationLock struct{ file *os.File }

func AcquireMutationLock(dataRoot string) (*MutationLock, error) {
	// The inode, not a pathname or launcher environment, identifies shared
	// staging and receipts. A lock file could be unlinked by data cleanup and
	// bypassed; a directory flock survives that deletion until its holder exits.
	descriptor, err := syscall.Open(dataRoot, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open data mutation directory: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), dataRoot)
	if err := syscall.Flock(descriptor, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w for %s", ErrMutationBusy, dataRoot)
		}
		return nil, fmt.Errorf("lock data mutation directory: %w", err)
	}
	return &MutationLock{file: file}, nil
}

// Close releases ownership even after a failed or cancelled operation. The
// close-on-exec descriptor is not inherited by children; process exit also
// releases ownership without stale on-disk state.
func (lock *MutationLock) Close() error {
	return lock.file.Close()
}
