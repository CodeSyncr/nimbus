package storage

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// MemoryDriver keeps files in memory (driver: memory). Useful in tests and
// for throwaway local runs; contents are lost when the process exits.
type MemoryDriver struct {
	mu    sync.RWMutex
	files map[string][]byte
}

// NewMemoryDriver returns an empty in-memory driver.
func NewMemoryDriver() *MemoryDriver {
	return &MemoryDriver{files: map[string][]byte{}}
}

// key normalizes a path the way LocalDriver resolves it, so "a/../b.txt",
// "/b.txt" and "b.txt" name the same file.
func (d *MemoryDriver) key(p string) string {
	return strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(p)), "/")
}

// Put stores the reader's contents at path.
func (d *MemoryDriver) Put(p string, src io.Reader) error {
	data, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.files[d.key(p)] = data
	d.mu.Unlock()
	return nil
}

// Get returns the file at path; a missing file returns an error matching fs.ErrNotExist.
func (d *MemoryDriver) Get(p string) (io.ReadCloser, error) {
	d.mu.RLock()
	data, ok := d.files[d.key(p)]
	d.mu.RUnlock()
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Delete removes the file at path; a missing file returns an error matching fs.ErrNotExist.
func (d *MemoryDriver) Delete(p string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := d.key(p)
	if _, ok := d.files[k]; !ok {
		return &fs.PathError{Op: "remove", Path: p, Err: fs.ErrNotExist}
	}
	delete(d.files, k)
	return nil
}

// Exists returns whether a file is stored at path.
func (d *MemoryDriver) Exists(p string) (bool, error) {
	d.mu.RLock()
	_, ok := d.files[d.key(p)]
	d.mu.RUnlock()
	return ok, nil
}

// Paths returns every stored path, sorted.
func (d *MemoryDriver) Paths() []string {
	d.mu.RLock()
	out := make([]string, 0, len(d.files))
	for k := range d.files {
		out = append(out, k)
	}
	d.mu.RUnlock()
	sort.Strings(out)
	return out
}
