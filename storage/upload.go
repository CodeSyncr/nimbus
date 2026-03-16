package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"crypto/rand"
)

// ── Upload Helpers ──────────────────────────────────────────────

// UploadedFile represents a file received via multipart form upload.
type UploadedFile struct {
	Header *multipart.FileHeader
	file   multipart.File
	opened bool
}

// NewUploadedFile wraps a multipart file header.
func NewUploadedFile(fh *multipart.FileHeader) *UploadedFile {
	return &UploadedFile{Header: fh}
}

// Open opens the file for reading.
func (u *UploadedFile) Open() (multipart.File, error) {
	if u.opened && u.file != nil {
		return u.file, nil
	}
	f, err := u.Header.Open()
	if err != nil {
		return nil, err
	}
	u.file = f
	u.opened = true
	return f, nil
}

// Name returns the original filename.
func (u *UploadedFile) Name() string {
	return u.Header.Filename
}

// Size returns the file size in bytes.
func (u *UploadedFile) Size() int64 {
	return u.Header.Size
}

// Extension returns the file extension (e.g. ".jpg").
func (u *UploadedFile) Extension() string {
	return strings.ToLower(filepath.Ext(u.Header.Filename))
}

// MimeType detects the MIME type by reading the first 512 bytes.
func (u *UploadedFile) MimeType() (string, error) {
	f, err := u.Open()
	if err != nil {
		return "", err
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if seeker, ok := f.(io.Seeker); ok {
		seeker.Seek(0, io.SeekStart)
	}
	return http.DetectContentType(buf[:n]), nil
}

// Store saves the file to the given driver at the specified directory.
// Returns the full path where the file was stored.
func (u *UploadedFile) Store(driver Driver, dir string) (string, error) {
	return u.StoreAs(driver, dir, u.Header.Filename)
}

// StoreAs saves the file with a custom name.
func (u *UploadedFile) StoreAs(driver Driver, dir, name string) (string, error) {
	f, err := u.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	path := filepath.Join(dir, name)
	return path, driver.Put(path, f)
}

// StoreRandomName saves the file with a randomly generated name, preserving the extension.
func (u *UploadedFile) StoreRandomName(driver Driver, dir string) (string, error) {
	ext := u.Extension()
	name := randomHex(16) + ext
	return u.StoreAs(driver, dir, name)
}

// IsValid checks if the file is not empty and has a valid header.
func (u *UploadedFile) IsValid() bool {
	return u.Header != nil && u.Header.Size > 0
}

// ── PutFromRequest ──────────────────────────────────────────────

// PutFromRequest extracts a file from a multipart request by field name
// and stores it using the given driver.
func PutFromRequest(r *http.Request, field string, driver Driver, dir string) (string, error) {
	file, header, err := r.FormFile(field)
	if err != nil {
		return "", fmt.Errorf("storage: failed to read form file %q: %w", field, err)
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	name := randomHex(16) + ext
	path := filepath.Join(dir, name)

	return path, driver.Put(path, file)
}

// PutFromRequestAs extracts a file and stores it with a specific name.
func PutFromRequestAs(r *http.Request, field string, driver Driver, dir, name string) (string, error) {
	file, _, err := r.FormFile(field)
	if err != nil {
		return "", fmt.Errorf("storage: failed to read form file %q: %w", field, err)
	}
	defer file.Close()

	path := filepath.Join(dir, name)
	return path, driver.Put(path, file)
}

// ── Signed URLs ─────────────────────────────────────────────────

// SignedURLGenerator can produce time-limited signed URLs for stored files.
type SignedURLGenerator struct {
	Secret  string // HMAC secret key
	BaseURL string // e.g. "https://example.com/files"
}

// NewSignedURLGenerator creates a generator with the given secret and base URL.
func NewSignedURLGenerator(secret, baseURL string) *SignedURLGenerator {
	return &SignedURLGenerator{Secret: secret, BaseURL: baseURL}
}

// TemporaryURL generates a signed URL that expires after the given duration.
//
//	url := gen.TemporaryURL("avatars/photo.jpg", 15*time.Minute)
//	// => https://example.com/files/avatars/photo.jpg?expires=1700000000&signature=abc123
func (g *SignedURLGenerator) TemporaryURL(path string, expiry time.Duration) string {
	expires := time.Now().Add(expiry).Unix()
	signature := g.sign(path, expires)

	sep := "?"
	if strings.Contains(g.BaseURL, "?") {
		sep = "&"
	}

	return fmt.Sprintf("%s/%s%sexpires=%d&signature=%s",
		strings.TrimRight(g.BaseURL, "/"),
		strings.TrimLeft(path, "/"),
		sep, expires, signature,
	)
}

// Verify checks if a signed URL is still valid.
func (g *SignedURLGenerator) Verify(path, signature string, expires int64) bool {
	if time.Now().Unix() > expires {
		return false
	}
	expected := g.sign(path, expires)
	return hmac.Equal([]byte(signature), []byte(expected))
}

func (g *SignedURLGenerator) sign(path string, expires int64) string {
	msg := fmt.Sprintf("%s:%d", path, expires)
	mac := hmac.New(sha256.New, []byte(g.Secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// ServeSignedFiles returns an http.HandlerFunc that verifies signed URLs
// and serves files from the given driver.
//
//	mux.HandleFunc("/files/", storage.ServeSignedFiles(driver, gen))
func ServeSignedFiles(driver Driver, gen *SignedURLGenerator, prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, prefix)
		path = strings.TrimLeft(path, "/")

		signature := r.URL.Query().Get("signature")
		expiresStr := r.URL.Query().Get("expires")
		if signature == "" || expiresStr == "" {
			http.Error(w, "missing signature", http.StatusForbidden)
			return
		}

		var expires int64
		fmt.Sscanf(expiresStr, "%d", &expires)

		if !gen.Verify(path, signature, expires) {
			http.Error(w, "invalid or expired signature", http.StatusForbidden)
			return
		}

		rc, err := driver.Get(path)
		if err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		defer rc.Close()

		// Detect content type.
		buf := make([]byte, 512)
		n, _ := rc.Read(buf)
		w.Header().Set("Content-Type", http.DetectContentType(buf[:n]))
		w.Write(buf[:n])
		io.Copy(w, rc)
	}
}

// ── File Validation ─────────────────────────────────────────────

// AllowedExtensions checks if the uploaded file has an allowed extension.
func AllowedExtensions(u *UploadedFile, exts ...string) bool {
	ext := u.Extension()
	for _, allowed := range exts {
		if strings.EqualFold(ext, allowed) || strings.EqualFold(ext, "."+allowed) {
			return true
		}
	}
	return false
}

// MaxFileSize checks if the uploaded file is within the size limit (in bytes).
func MaxFileSize(u *UploadedFile, maxBytes int64) bool {
	return u.Size() <= maxBytes
}

// ── Helpers ─────────────────────────────────────────────────────

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
