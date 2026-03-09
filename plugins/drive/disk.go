/*
|--------------------------------------------------------------------------
| Drive Disk Interface
|--------------------------------------------------------------------------
|
| Disk is the unified interface for file storage operations across
| local filesystem, S3, GCS, and other providers. Inspired by AdonisJS Drive.
|
*/

package drive

import (
	"io"
	"time"
)

// Visibility determines file access: public (URL) or private (signed URL only).
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// Disk is the interface for file storage operations.
type Disk interface {
	// Put writes content to the given path/key.
	Put(path string, src io.Reader) error

	// Get returns a reader for the file at path. Caller must close it.
	Get(path string) (io.ReadCloser, error)

	// Delete removes the file at path.
	Delete(path string) error

	// Exists returns true if the file exists.
	Exists(path string) (bool, error)

	// GetUrl returns the public URL for the file (public visibility).
	// For local fs with serveFiles, returns path like /uploads/key.
	// For S3/GCS, returns the full URL.
	GetUrl(path string) (string, error)

	// GetSignedUrl returns a temporary signed URL (private visibility).
	// expiresIn is the duration until the URL expires.
	GetSignedUrl(path string, expiresIn time.Duration) (string, error)
}
