package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"paracetamol/internal/controlerr"
)

// Keep both directory handles across confirmation. A pathname check alone
// cannot stop a subsequent ancestor rename from redirecting recursive deletion.
type cleanupDirectory struct {
	path      string
	name      string
	parent    *os.Root
	directory *os.Root
	identity  os.FileInfo
	size      int64
}

func planCleanupDirectory(path string) (_ *cleanupDirectory, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if path == string(filepath.Separator) {
		return nil, controlerr.New("refusing cleanup of filesystem root")
	}
	if err := rejectSymlinkComponents(path); err != nil {
		return nil, err
	}
	identity, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !identity.IsDir() || identity.Mode()&os.ModeSymlink != 0 {
		return nil, controlerr.New("cleanup target is not a real directory: %s", path)
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	target := &cleanupDirectory{path: path, name: filepath.Base(path), parent: parent, identity: identity}
	defer func() {
		if err != nil {
			target.close()
		}
	}()
	target.directory, err = parent.OpenRoot(target.name)
	if err != nil {
		return nil, err
	}
	opened, err := target.directory.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(identity, opened) {
		return nil, target.changed()
	}
	if err = target.validate(); err != nil {
		return nil, err
	}
	err = fs.WalkDir(target.directory.FS(), ".", func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			target.size += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return target, nil
}

func (target *cleanupDirectory) close() {
	if target.directory != nil {
		_ = target.directory.Close()
	}
	_ = target.parent.Close()
}

func (target *cleanupDirectory) changed() error {
	return controlerr.New("cleanup directory changed since planning: %s; review a new cleanup plan", target.path)
}

func (target *cleanupDirectory) validate() error {
	if err := rejectSymlinkComponents(target.path); err != nil {
		return err
	}
	currentParent, err := os.Lstat(filepath.Dir(target.path))
	if err != nil {
		return target.changed()
	}
	boundParent, err := target.parent.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(currentParent, boundParent) {
		return target.changed()
	}
	current, err := target.parent.Lstat(target.name)
	if err != nil || !current.IsDir() || !os.SameFile(current, target.identity) {
		return target.changed()
	}
	return nil
}

func (target *cleanupDirectory) remove() error {
	if err := target.validate(); err != nil {
		return err
	}
	if err := target.clearContents(); err != nil {
		return err
	}
	if err := target.validate(); err != nil {
		return err
	}
	// Only remove the empty root here. Never recursively traverse its name
	// again after deleting contents through the held directory handle.
	return removeEmptyCleanupDirectory(target.parent, target.name)
}

func (target *cleanupDirectory) clearContents() error {
	const batchSize = 128
	for {
		entries, err := target.directory.Open(".")
		if err != nil {
			return err
		}
		names, readErr := entries.Readdirnames(batchSize)
		closeErr := entries.Close()
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		for _, name := range names {
			// Traverse only the held target, never its potentially replaced path.
			if err := target.directory.RemoveAll(name); err != nil {
				return fmt.Errorf("remove entry %s: %w", name, err)
			}
		}
		if len(names) < batchSize {
			return nil
		}
		// Deleting entries can reshuffle a directory's iteration offsets.
		// Like os.RemoveAll, reopen after each batch rather than skip entries.
	}
}
