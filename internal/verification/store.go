// Package verification preserves durable successful content hashes.
package verification

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"rocmplete/internal/storage"
)

const SchemaVersion = 1

type Record struct {
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	CTimeNS    int64  `json:"st_ctime_ns"`
	Device     uint64 `json:"st_dev"`
	Inode      uint64 `json:"st_ino"`
	ModifiedNS int64  `json:"st_mtime_ns"`
}

type document struct {
	Files  map[string]Record `json:"files"`
	Schema int               `json:"schema"`
}

type Store struct {
	DataRoot string
	Path     string
	Records  map[string]Record
	Changed  bool
}

func Load(dataRoot string) (*Store, error) {
	path := (storage.Layout{Root: dataRoot}).VerificationReceipt()
	status, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return &Store{DataRoot: dataRoot, Path: path, Records: make(map[string]Record)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect content verification receipt %s: %w", path, err)
	}
	if status.Mode()&os.ModeSymlink != 0 || !status.Mode().IsRegular() {
		return nil, fmt.Errorf("content verification receipt is not a regular file: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read content verification receipt %s: %w", path, err)
	}
	var raw document
	if err := json.Unmarshal(contents, &raw); err != nil {
		return &Store{DataRoot: dataRoot, Path: path, Records: make(map[string]Record)}, fmt.Errorf("cannot read content verification receipt %s: %w", path, err)
	}
	if raw.Schema != SchemaVersion || raw.Files == nil {
		return nil, fmt.Errorf("unsupported content verification receipt schema in %s", path)
	}
	return &Store{DataRoot: dataRoot, Path: path, Records: raw.Files}, nil
}

func (store *Store) key(file string) (string, error) {
	root, err := filepath.EvalSymlinks(store.DataRoot)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", fmt.Errorf("resolve managed content: %w", err)
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("managed content path is outside the data directory: %s", file)
	}
	return filepath.ToSlash(relative), nil
}

func signature(info os.FileInfo) (Record, error) {
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Record{}, fmt.Errorf("unsupported filesystem stat result")
	}
	return Record{Size: info.Size(), CTimeNS: status.Ctim.Sec*1e9 + status.Ctim.Nsec, Device: uint64(status.Dev), Inode: status.Ino, ModifiedNS: status.Mtim.Sec*1e9 + status.Mtim.Nsec}, nil
}

func (store *Store) Matches(file string, size int64, sha256 string) bool {
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return false
	}
	key, err := store.key(file)
	if err != nil {
		return false
	}
	expected, err := signature(info)
	if err != nil {
		return false
	}
	expected.SHA256 = sha256
	actual, ok := store.Records[key]
	return ok && actual == expected
}

func (store *Store) Record(file string, size int64, sha256 string) error {
	info, err := os.Lstat(file)
	if err != nil {
		return fmt.Errorf("record content verification for %s: %w", file, err)
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return fmt.Errorf("cannot record verification for unexpected content file: %s", file)
	}
	key, err := store.key(file)
	if err != nil {
		return err
	}
	record, err := signature(info)
	if err != nil {
		return err
	}
	record.SHA256 = sha256
	if store.Records[key] != record {
		store.Records[key] = record
		store.Changed = true
	}
	return nil
}

func (store *Store) Save() error {
	if !store.Changed {
		return nil
	}
	parent := filepath.Dir(store.Path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("prepare verification directory %s: %w", parent, err)
	}
	status, err := os.Lstat(parent)
	if err != nil || status.Mode()&os.ModeSymlink != 0 || !status.IsDir() {
		return fmt.Errorf("content verification directory is not a directory: %s", parent)
	}
	encoded, err := json.MarshalIndent(document{Files: store.Records, Schema: SchemaVersion}, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(parent, ".verification-*.tmp")
	if err != nil {
		return fmt.Errorf("create verification receipt: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, store.Path); err != nil {
		return fmt.Errorf("replace verification receipt: %w", err)
	}
	committed = true
	directory, err := os.Open(parent)
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	store.Changed = false
	return nil
}
