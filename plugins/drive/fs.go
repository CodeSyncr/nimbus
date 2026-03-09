/*
|--------------------------------------------------------------------------
| Drive FS (Local) Driver
|--------------------------------------------------------------------------
|
| Stores files on the local filesystem. When ServeFiles is true,
| registers a route to serve files via HTTP (e.g. /uploads/*).
|
*/

package drive

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/storage"
)

// FSDisk implements Disk for local filesystem storage.
type FSDisk struct {
	driver   *storage.LocalDriver
	baseURL  string // e.g. "http://localhost:3333"
	basePath string // e.g. "/uploads"
}

// NewFSDisk creates a new local filesystem disk.
func NewFSDisk(location, baseURL, routeBasePath string) *FSDisk {
	absRoot, _ := filepath.Abs(location)
	if absRoot == "" {
		absRoot = location
	}
	return &FSDisk{
		driver:   storage.NewLocalDriver(absRoot),
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		basePath: strings.TrimSuffix(routeBasePath, "/"),
	}
}

// Put writes content to path.
func (d *FSDisk) Put(path string, src io.Reader) error {
	return d.driver.Put(path, src)
}

// Get returns a reader for the file.
func (d *FSDisk) Get(path string) (io.ReadCloser, error) {
	return d.driver.Get(path)
}

// Delete removes the file.
func (d *FSDisk) Delete(path string) error {
	return d.driver.Delete(path)
}

// Exists returns true if the file exists.
func (d *FSDisk) Exists(path string) (bool, error) {
	return d.driver.Exists(path)
}

// GetUrl returns the public URL for the file.
func (d *FSDisk) GetUrl(path string) (string, error) {
	cleanPath := filepath.ToSlash(path)
	if d.baseURL != "" {
		return d.baseURL + d.basePath + "/" + cleanPath, nil
	}
	return d.basePath + "/" + cleanPath, nil
}

// GetSignedUrl returns a temporary URL. For local fs, we just return the public URL
// since we don't have signed URL support. In production with private files,
// you'd want to generate a token-based URL.
func (d *FSDisk) GetSignedUrl(path string, expiresIn time.Duration) (string, error) {
	_ = expiresIn
	return d.GetUrl(path)
}

// ContentType returns a MIME type for the path based on extension.
func ContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// SafePath validates path and prevents traversal. Returns cleaned path or error.
func SafePath(path string) (string, error) {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		decoded = path
	}
	if strings.Contains(decoded, "..") {
		return "", os.ErrNotExist
	}
	return decoded, nil
}

// Ensure FSDisk implements Disk.
var _ Disk = (*FSDisk)(nil)
