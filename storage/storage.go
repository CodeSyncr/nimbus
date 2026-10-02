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

// resolve maps a storage path to a file under Root. Cleaning it as an
// absolute path first drops any leading "..", so paths built from user
// input ("../../etc/passwd") cannot reach outside Root.
func (d *LocalDriver) resolve(path string) string {
	return filepath.Join(d.Root, filepath.Clean(string(filepath.Separator)+path))
}

// Put writes the reader to root/path, creating parent dirs. The file is
// written to a temporary name and renamed, so readers never see a partial
// file and a failed copy leaves the previous version in place.
func (d *LocalDriver) Put(path string, src io.Reader) error {
	full := d.resolve(path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(full), ".tmp-"+filepath.Base(full)+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = io.Copy(f, src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0644)
	}
	if err == nil {
		err = os.Rename(tmp, full)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// Get opens the file at root/path for reading.
func (d *LocalDriver) Get(path string) (io.ReadCloser, error) {
	return os.Open(d.resolve(path))
}

// Delete removes the file at root/path.
func (d *LocalDriver) Delete(path string) error {
	return os.Remove(d.resolve(path))
}

// Exists returns whether the file exists.
func (d *LocalDriver) Exists(path string) (bool, error) {
	_, err := os.Stat(d.resolve(path))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
