package supabase

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// encodePath URL-encodes each segment of a storage path while
// preserving the "/" separators between them.
func encodePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// StorageClient provides access to Supabase Storage buckets and objects.
type StorageClient struct {
	client *Client
}

// Bucket represents a Supabase Storage bucket.
type Bucket struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Public    bool   `json:"public"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// StorageObject represents a file in a bucket.
type StorageObject struct {
	Name      string         `json:"name"`
	ID        string         `json:"id,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SignedURLResult is the response from createSignedUrl.
type SignedURLResult struct {
	SignedURL string `json:"signedURL"`
}

// BucketHandle provides operations on a specific storage bucket.
type BucketHandle struct {
	storage *StorageClient
	name    string
}

// From returns a handle to the named bucket.
//
//	client.Storage.From("avatars").Upload("user1.png", file)
func (s *StorageClient) From(bucket string) *BucketHandle {
	return &BucketHandle{storage: s, name: bucket}
}

// ListBuckets returns all storage buckets.
func (s *StorageClient) ListBuckets() ([]Bucket, error) {
	resp, err := s.client.doJSON("GET", s.url("/bucket"), nil)
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]Bucket](resp)
}

// CreateBucket creates a new storage bucket.
func (s *StorageClient) CreateBucket(name string, public bool) error {
	resp, err := s.client.doJSON("POST", s.url("/bucket"), map[string]any{
		"name":   name,
		"public": public,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// DeleteBucket deletes an empty bucket.
func (s *StorageClient) DeleteBucket(name string) error {
	resp, err := s.client.doJSON("DELETE", s.url("/bucket/"+name), nil)
	if err != nil {
		return err
	}
	return drainResp(resp)
}

func (s *StorageClient) url(path string) string {
	return s.client.url + "/storage/v1" + path
}

// Upload uploads a file to the bucket. contentType is optional (defaults to application/octet-stream).
func (b *BucketHandle) Upload(path string, body io.Reader, contentType ...string) error {
	ct := "application/octet-stream"
	if len(contentType) > 0 && contentType[0] != "" {
		ct = contentType[0]
	}
	u := b.storage.url("/object/" + b.name + "/" + encodePath(path))
	resp, err := b.storage.client.do("POST", u, body, map[string]string{
		"Content-Type": ct,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// Update replaces an existing file in the bucket.
func (b *BucketHandle) Update(path string, body io.Reader, contentType ...string) error {
	ct := "application/octet-stream"
	if len(contentType) > 0 && contentType[0] != "" {
		ct = contentType[0]
	}
	u := b.storage.url("/object/" + b.name + "/" + encodePath(path))
	resp, err := b.storage.client.do("PUT", u, body, map[string]string{
		"Content-Type": ct,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// Download returns a reader for the file. Caller must close the returned ReadCloser.
func (b *BucketHandle) Download(path string) (io.ReadCloser, error) {
	u := b.storage.url("/object/" + b.name + "/" + encodePath(path))
	resp, err := b.storage.client.do("GET", u, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("supabase: storage download %s: %d %s", path, resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

// Remove deletes one or more files from the bucket.
func (b *BucketHandle) Remove(paths []string) error {
	u := b.storage.url("/object/" + b.name)
	resp, err := b.storage.client.doJSON("DELETE", u, map[string]any{
		"prefixes": paths,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// List returns objects in the bucket under the given prefix.
func (b *BucketHandle) List(prefix string, opts ...ListOption) ([]StorageObject, error) {
	o := listOpts{Prefix: prefix, Limit: 100, Offset: 0}
	for _, fn := range opts {
		fn(&o)
	}
	u := b.storage.url("/object/list/" + b.name)
	resp, err := b.storage.client.doJSON("POST", u, map[string]any{
		"prefix": o.Prefix,
		"limit":  o.Limit,
		"offset": o.Offset,
	})
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]StorageObject](resp)
}

// GetPublicURL returns the public URL for a file (bucket must be public).
func (b *BucketHandle) GetPublicURL(path string) string {
	return b.storage.client.url + "/storage/v1/object/public/" + b.name + "/" + encodePath(path)
}

// CreateSignedURL returns a time-limited signed URL for private access.
func (b *BucketHandle) CreateSignedURL(path string, expiresIn time.Duration) (string, error) {
	u := b.storage.url("/object/sign/" + b.name + "/" + encodePath(path))
	resp, err := b.storage.client.doJSON("POST", u, map[string]any{
		"expiresIn": int(expiresIn.Seconds()),
	})
	if err != nil {
		return "", err
	}
	result, err := decodeJSON[SignedURLResult](resp)
	if err != nil {
		return "", err
	}
	signed := result.SignedURL
	if !strings.HasPrefix(signed, "http") {
		signed = b.storage.client.url + "/storage/v1" + signed
	}
	return signed, nil
}

// Move renames/moves a file within the bucket.
func (b *BucketHandle) Move(from, to string) error {
	u := b.storage.url("/object/move")
	resp, err := b.storage.client.doJSON("POST", u, map[string]any{
		"bucketId":       b.name,
		"sourceKey":      from,
		"destinationKey": to,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// Copy copies a file within the bucket.
func (b *BucketHandle) Copy(from, to string) error {
	u := b.storage.url("/object/copy")
	resp, err := b.storage.client.doJSON("POST", u, map[string]any{
		"bucketId":       b.name,
		"sourceKey":      from,
		"destinationKey": to,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// GetDownloadURL returns a public download URL with download disposition.
func (b *BucketHandle) GetDownloadURL(path string) string {
	return b.storage.client.url + "/storage/v1/object/public/" + b.name + "/" + encodePath(path) + "?download="
}

// GetTransformURL returns a URL for image transformations (Supabase Image Transformation).
func (b *BucketHandle) GetTransformURL(path string, opts TransformOptions) string {
	params := url.Values{}
	if opts.Width > 0 {
		params.Set("width", strconv.Itoa(opts.Width))
	}
	if opts.Height > 0 {
		params.Set("height", strconv.Itoa(opts.Height))
	}
	if opts.Quality > 0 {
		params.Set("quality", strconv.Itoa(opts.Quality))
	}
	if opts.Format != "" {
		params.Set("format", opts.Format)
	}
	if opts.Resize != "" {
		params.Set("resize", string(opts.Resize))
	}
	return b.storage.client.url + "/storage/v1/render/image/public/" + b.name + "/" + encodePath(path) + "?" + params.Encode()
}

// TransformOptions for Supabase image transformations.
type TransformOptions struct {
	Width   int
	Height  int
	Quality int
	Format  string
	Resize  ResizeMode
}

type ResizeMode string

const (
	ResizeCover   ResizeMode = "cover"
	ResizeContain ResizeMode = "contain"
	ResizeFill    ResizeMode = "fill"
)

type listOpts struct {
	Prefix string
	Limit  int
	Offset int
}

type ListOption func(*listOpts)

func WithLimit(n int) ListOption  { return func(o *listOpts) { o.Limit = n } }
func WithOffset(n int) ListOption { return func(o *listOpts) { o.Offset = n } }
