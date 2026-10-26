package storage

import (
	"bytes"
	"crypto/rand"
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

// resolve preserves the historical normalization of ../ paths, returning a
// relative name for os.Root. Root enforces confinement during each operation,
// including directory symlinks; lexical cleaning alone cannot do that.
func (d *LocalDriver) resolve(name string) string {
	return strings.TrimPrefix(filepath.Clean(string(filepath.Separator)+name), string(filepath.Separator))
}

func (d *LocalDriver) Put(name string, src io.Reader) error {
	if err := os.MkdirAll(d.Root, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(d.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	name = d.resolve(name)
	if err := root.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(name), ".tmp-"+rand.Text())
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = io.Copy(f, src)
	if err == nil {
		err = f.Chmod(0644)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return root.Rename(tmp, name)
}

func (d *LocalDriver) Get(name string) (io.ReadCloser, error) {
	root, err := os.OpenRoot(d.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(d.resolve(name))
}
func (d *LocalDriver) Delete(name string) error {
	root, err := os.OpenRoot(d.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(d.resolve(name))
}
func (d *LocalDriver) Exists(name string) (bool, error) {
	root, err := os.OpenRoot(d.Root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer root.Close()
	_, err = root.Stat(d.resolve(name))
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
