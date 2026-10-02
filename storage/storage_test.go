package storage

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func readAll(t *testing.T, d Driver, path string) string {
	t.Helper()
	rc, err := d.Get(path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b)
}

func TestLocalDriverCRUD(t *testing.T) {
	d := NewLocalDriver(t.TempDir())
	if err := d.Put("a/b/c.txt", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, d, "a/b/c.txt"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if ok, err := d.Exists("a/b/c.txt"); !ok || err != nil {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	if err := d.Put("a/b/c.txt", strings.NewReader("replaced")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, d, "a/b/c.txt"); got != "replaced" {
		t.Fatalf("overwrite: got %q", got)
	}
	if err := d.Delete("a/b/c.txt"); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.Exists("a/b/c.txt"); ok || err != nil {
		t.Fatalf("Exists after delete = %v, %v", ok, err)
	}
}

func TestLocalDriverStaysInsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	d := NewLocalDriver(root)
	if err := d.Put("../../escaped.txt", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); err == nil {
		t.Fatal("Put wrote outside the storage root")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.txt")); err != nil {
		t.Fatalf("traversal path should land inside root: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestLocalDriverFailedPutKeepsPreviousFile(t *testing.T) {
	root := t.TempDir()
	d := NewLocalDriver(root)
	_ = d.Put("f.txt", strings.NewReader("good"))
	if err := d.Put("f.txt", io.MultiReader(strings.NewReader("partial"), failingReader{})); err == nil {
		t.Fatal("expected copy error")
	}
	if got := readAll(t, d, "f.txt"); got != "good" {
		t.Fatalf("failed Put clobbered the file: %q", got)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func multipartRequest(t *testing.T, field, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile(field, filename)
	_, _ = fw.Write(content)
	_ = w.Close()
	r := httptest.NewRequest("POST", "/upload", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func uploaded(t *testing.T, filename string, content []byte) *UploadedFile {
	t.Helper()
	r := multipartRequest(t, "file", filename, content)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	return NewUploadedFile(r.MultipartForm.File["file"][0])
}

func TestUploadedFile(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
	u := uploaded(t, "Photo.PNG", png)
	if u.Name() != "Photo.PNG" || u.Extension() != ".png" || u.Size() != int64(len(png)) || !u.IsValid() {
		t.Fatalf("name=%q ext=%q size=%d", u.Name(), u.Extension(), u.Size())
	}
	if mt, err := u.MimeType(); err != nil || mt != "image/png" {
		t.Fatalf("MimeType = %q, %v", mt, err)
	}
	if !AllowedExtensions(u, "jpg", ".png") || AllowedExtensions(u, "gif") {
		t.Fatal("AllowedExtensions wrong")
	}
	if !MaxFileSize(u, 1000) || MaxFileSize(u, 10) {
		t.Fatal("MaxFileSize wrong")
	}

	d := NewLocalDriver(t.TempDir())
	path, err := uploaded(t, "Photo.PNG", png).StoreRandomName(d, "avatars")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "avatars/") || !strings.HasSuffix(path, ".png") || len(filepath.Base(path)) != 36 {
		t.Fatalf("random path = %q", path)
	}
	// MimeType must not consume the bytes that get stored.
	if got := readAll(t, d, path); got != string(png) {
		t.Fatalf("stored %d bytes, want %d", len(got), len(png))
	}
	if path, _ := uploaded(t, "doc.txt", []byte("hi")).Store(d, "docs"); readAll(t, d, path) != "hi" {
		t.Fatal("Store did not keep the original name")
	}
}

func TestPutFromRequest(t *testing.T) {
	d := NewLocalDriver(t.TempDir())
	path, err := PutFromRequest(multipartRequest(t, "file", "a.TXT", []byte("one")), "file", d, "in")
	if err != nil || !strings.HasSuffix(path, ".txt") || readAll(t, d, path) != "one" {
		t.Fatalf("PutFromRequest = %q, %v", path, err)
	}
	path, err = PutFromRequestAs(multipartRequest(t, "file", "a.txt", []byte("two")), "file", d, "in", "named.txt")
	if err != nil || path != filepath.Join("in", "named.txt") || readAll(t, d, path) != "two" {
		t.Fatalf("PutFromRequestAs = %q, %v", path, err)
	}
	if _, err := PutFromRequest(multipartRequest(t, "other", "a.txt", nil), "file", d, "in"); err == nil {
		t.Fatal("missing field should fail")
	}
}

func TestSignedURLs(t *testing.T) {
	g := NewSignedURLGenerator("secret", "https://example.com/files/")
	raw := g.TemporaryURL("/avatars/me.jpg", time.Minute)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/files/avatars/me.jpg" {
		t.Fatalf("path = %q", u.Path)
	}
	exp, _ := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
	sig := u.Query().Get("signature")
	if !g.Verify("avatars/me.jpg", sig, exp) {
		t.Fatal("valid signature rejected")
	}
	if g.Verify("avatars/other.jpg", sig, exp) {
		t.Fatal("signature accepted for another path")
	}
	if NewSignedURLGenerator("other", "").Verify("avatars/me.jpg", sig, exp) {
		t.Fatal("signature accepted with another secret")
	}
	if g.Verify("avatars/me.jpg", g.sign("avatars/me.jpg", 1), 1) {
		t.Fatal("expired signature accepted")
	}
	if q := NewSignedURLGenerator("s", "https://cdn.example/f?v=1").TemporaryURL("x", time.Minute); !strings.HasPrefix(q, "https://cdn.example/f/x?") || !strings.Contains(q, "v=1") {
		t.Fatalf("existing query not preserved: %s", q)
	}
}

func TestServeSignedFiles(t *testing.T) {
	d := NewLocalDriver(t.TempDir())
	_ = d.Put("docs/a.txt", strings.NewReader("secret contents"))
	g := NewSignedURLGenerator("k", "http://x/files")
	h := ServeSignedFiles(d, g, "/files")

	u, _ := url.Parse(g.TemporaryURL("docs/a.txt", time.Minute))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", u.RequestURI(), nil))
	if rec.Code != 200 || rec.Body.String() != "secret contents" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status=%d body=%q type=%q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	for _, target := range []string{
		"/files/docs/a.txt",
		"/files/docs/a.txt?expires=9999999999&signature=forged",
	} {
		rec = httptest.NewRecorder()
		h(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", target, rec.Code)
		}
	}
	u, _ = url.Parse(g.TemporaryURL("docs/missing.txt", time.Minute))
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", u.RequestURI(), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing file: status %d", rec.Code)
	}
}
