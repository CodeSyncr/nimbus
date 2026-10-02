package storage

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeS3 implements the handful of path-style S3 calls the driver makes.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    bool
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	key := ""
	if len(parts) == 2 {
		key = parts[1]
	}
	switch {
	case r.Method == http.MethodGet && key == "":
		type content struct{ Key string }
		var res struct {
			XMLName  xml.Name  `xml:"ListBucketResult"`
			Contents []content `xml:"Contents"`
		}
		prefix := r.URL.Query().Get("prefix")
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			res.Contents = append(res.Contents, content{Key: k})
		}
		_ = xml.NewEncoder(w).Encode(res)
	case r.Method == http.MethodPut && r.Header.Get("X-Amz-Copy-Source") != "":
		src := strings.SplitN(strings.TrimPrefix(r.Header.Get("X-Amz-Copy-Source"), "/"), "/", 2)[1]
		f.objects[key] = f.objects[src]
		_, _ = w.Write([]byte(`<CopyObjectResult><ETag>"x"</ETag></CopyObjectResult>`))
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[key] = body
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
			}
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Last-Modified", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func newFakeS3Driver(t *testing.T) (*S3Driver, *fakeS3) {
	t.Helper()
	fake := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := s3.New(s3.Options{
		Region:                     "us-east-1",
		BaseEndpoint:               aws.String(srv.URL),
		UsePathStyle:               true,
		Credentials:                aws.AnonymousCredentials{},
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		RetryMaxAttempts:           1,
	})
	return NewS3Driver(S3Config{Client: client, Bucket: "bucket", RequestTimeout: 5 * time.Second}), fake
}

func TestS3DriverObjectLifecycle(t *testing.T) {
	d, _ := newFakeS3Driver(t)
	if err := d.Put("docs/a.txt", strings.NewReader("hello s3")); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.Exists("docs/a.txt"); !ok || err != nil {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	if ok, err := d.Exists("docs/missing.txt"); ok || err != nil {
		t.Fatalf("Exists(missing) = %v, %v; want false, nil", ok, err)
	}
	if n, err := d.Size("docs/a.txt"); err != nil || n != 8 {
		t.Fatalf("Size = %d, %v", n, err)
	}
	if lm, err := d.LastModified("docs/a.txt"); err != nil || lm.Year() != 2026 {
		t.Fatalf("LastModified = %v, %v", lm, err)
	}
	if err := d.Move("docs/a.txt", "docs/b.txt"); err != nil {
		t.Fatal(err)
	}
	keys, err := d.List("docs/")
	if err != nil || len(keys) != 1 || keys[0] != "docs/b.txt" {
		t.Fatalf("List = %v, %v", keys, err)
	}
	if err := d.Delete("docs/b.txt"); err != nil {
		t.Fatal(err)
	}
	if d.URL("x/y.png") != "https://bucket.s3.amazonaws.com/x/y.png" {
		t.Fatalf("URL = %s", d.URL("x/y.png"))
	}
}

func TestS3DriverGetBodyOutlivesTheCall(t *testing.T) {
	d, _ := newFakeS3Driver(t)
	big := strings.Repeat("0123456789", 200_000) // 2 MB, more than one read buffer
	if err := d.Put("big.bin", strings.NewReader(big)); err != nil {
		t.Fatal(err)
	}
	rc, err := d.Get("big.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading the body after Get returned failed: %v", err)
	}
	if len(got) != len(big) {
		t.Fatalf("read %d bytes, want %d", len(got), len(big))
	}
	if _, err := d.Get("nope"); err == nil {
		t.Fatal("Get of a missing key should fail")
	}
}

func TestS3DriverExistsReportsRealErrors(t *testing.T) {
	d, fake := newFakeS3Driver(t)
	fake.fail = true
	if ok, err := d.Exists("anything"); ok || err == nil {
		t.Fatalf("Exists during an outage = %v, %v; want an error, not 'missing'", ok, err)
	}
}

func TestS3DriverPresignedURLs(t *testing.T) {
	d, _ := newFakeS3Driver(t)
	d.client = s3.New(s3.Options{Region: "us-east-1", Credentials: aws.NewCredentialsCache(staticCreds{})})
	get, err := d.TemporaryURL("a.txt", time.Minute)
	if err != nil || !strings.Contains(get, "X-Amz-Signature=") || !strings.Contains(get, "X-Amz-Expires=60") {
		t.Fatalf("TemporaryURL = %s, %v", get, err)
	}
	put, err := d.TemporaryUploadURL("a.txt", time.Minute)
	if err != nil || !strings.Contains(put, "X-Amz-Signature=") {
		t.Fatalf("TemporaryUploadURL = %s, %v", put, err)
	}
}

type staticCreds struct{}

func (staticCreds) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"}, nil
}
