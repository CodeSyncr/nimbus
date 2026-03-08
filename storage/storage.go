package storage

import (
	"io"
	"os"
	"path/filepath"
)

// Driver is the file storage interface (plan: storage.Put, storage.Get).
type Driver interface {
	Put(path string, src io.Reader) error
	Get(path string) (io.ReadCloser, error)
	Delete(path string) error
	Exists(path string) (bool, error)
}

// LocalDriver stores files on the local filesystem (driver: local).
type LocalDriver struct {
	Root string
}

// NewLocalDriver returns a driver that stores under root (e.g. "storage/app").
func NewLocalDriver(root string) *LocalDriver {
	return &LocalDriver{Root: root}
}

// Put writes the reader to root/path, creating parent dirs.
func (d *LocalDriver) Put(path string, src io.Reader) error {
	full := filepath.Join(d.Root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	f, err := os.Create(full)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

// Get opens the file at root/path for reading.
func (d *LocalDriver) Get(path string) (io.ReadCloser, error) {
	full := filepath.Join(d.Root, path)
	return os.Open(full)
}

// Delete removes the file at root/path.
func (d *LocalDriver) Delete(path string) error {
	return os.Remove(filepath.Join(d.Root, path))
}

// Exists returns whether the file exists.
func (d *LocalDriver) Exists(path string) (bool, error) {
	_, err := os.Stat(filepath.Join(d.Root, path))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
