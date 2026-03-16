package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Driver implements the Driver interface using Amazon S3 (or any S3-compatible store).
type S3Driver struct {
	client *s3.Client
	bucket string

	// presign is lazily created for signed URLs.
	presign *s3.PresignClient
}

// S3Config holds configuration for the S3 driver.
type S3Config struct {
	// Client is a pre-configured *s3.Client.
	Client *s3.Client

	// Bucket is the S3 bucket name.
	Bucket string
}

// NewS3Driver creates a new S3-backed storage driver.
func NewS3Driver(cfg S3Config) *S3Driver {
	return &S3Driver{
		client: cfg.Client,
		bucket: cfg.Bucket,
	}
}

// Put uploads src to the given path in the bucket.
func (d *S3Driver) Put(path string, src io.Reader) error {
	// Read all into memory to determine content type and allow PutObject
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("s3: read source: %w", err)
	}

	contentType := http.DetectContentType(data)

	_, err = d.client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(d.bucket),
		Key:         aws.String(path),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3: put %q: %w", path, err)
	}
	return nil
}

// PutWithOptions uploads with explicit content type and ACL.
func (d *S3Driver) PutWithOptions(path string, src io.Reader, contentType string, acl types.ObjectCannedACL) error {
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("s3: read source: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
		Body:   bytes.NewReader(data),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if acl != "" {
		input.ACL = acl
	}

	_, err = d.client.PutObject(context.TODO(), input)
	if err != nil {
		return fmt.Errorf("s3: put %q: %w", path, err)
	}
	return nil
}

// Get downloads the object at path and returns a ReadCloser.
func (d *S3Driver) Get(path string) (io.ReadCloser, error) {
	out, err := d.client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: get %q: %w", path, err)
	}
	return out.Body, nil
}

// Delete removes the object at path.
func (d *S3Driver) Delete(path string) error {
	_, err := d.client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return fmt.Errorf("s3: delete %q: %w", path, err)
	}
	return nil
}

// DeleteMany removes multiple objects in a single batch request.
func (d *S3Driver) DeleteMany(paths []string) error {
	objects := make([]types.ObjectIdentifier, len(paths))
	for i, p := range paths {
		objects[i] = types.ObjectIdentifier{Key: aws.String(p)}
	}
	_, err := d.client.DeleteObjects(context.TODO(), &s3.DeleteObjectsInput{
		Bucket: aws.String(d.bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("s3: delete many: %w", err)
	}
	return nil
}

// Exists checks if the object exists in the bucket.
func (d *S3Driver) Exists(path string) (bool, error) {
	_, err := d.client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		// Check if it's a NotFound-style error
		return false, nil
	}
	return true, nil
}

// Size returns the size in bytes of the object.
func (d *S3Driver) Size(path string) (int64, error) {
	out, err := d.client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return 0, fmt.Errorf("s3: size %q: %w", path, err)
	}
	if out.ContentLength != nil {
		return *out.ContentLength, nil
	}
	return 0, nil
}

// LastModified returns the last modified time of the object.
func (d *S3Driver) LastModified(path string) (time.Time, error) {
	out, err := d.client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("s3: lastModified %q: %w", path, err)
	}
	if out.LastModified != nil {
		return *out.LastModified, nil
	}
	return time.Time{}, nil
}

// Copy copies an object from src to dst within the same bucket.
func (d *S3Driver) Copy(src, dst string) error {
	_, err := d.client.CopyObject(context.TODO(), &s3.CopyObjectInput{
		Bucket:     aws.String(d.bucket),
		CopySource: aws.String(d.bucket + "/" + src),
		Key:        aws.String(dst),
	})
	if err != nil {
		return fmt.Errorf("s3: copy %q → %q: %w", src, dst, err)
	}
	return nil
}

// Move moves an object from src to dst (copy + delete).
func (d *S3Driver) Move(src, dst string) error {
	if err := d.Copy(src, dst); err != nil {
		return err
	}
	return d.Delete(src)
}

// URL returns the public URL of an object (assumes bucket is publicly accessible or behind CloudFront).
func (d *S3Driver) URL(path string) string {
	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", d.bucket, path)
}

// TemporaryURL returns a pre-signed URL valid for the given duration.
func (d *S3Driver) TemporaryURL(path string, duration time.Duration) (string, error) {
	if d.presign == nil {
		d.presign = s3.NewPresignClient(d.client)
	}

	req, err := d.presign.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	}, s3.WithPresignExpires(duration))
	if err != nil {
		return "", fmt.Errorf("s3: presign %q: %w", path, err)
	}
	return req.URL, nil
}

// TemporaryUploadURL returns a pre-signed PUT URL for direct uploads.
func (d *S3Driver) TemporaryUploadURL(path string, duration time.Duration) (string, error) {
	if d.presign == nil {
		d.presign = s3.NewPresignClient(d.client)
	}

	req, err := d.presign.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(path),
	}, s3.WithPresignExpires(duration))
	if err != nil {
		return "", fmt.Errorf("s3: presign upload %q: %w", path, err)
	}
	return req.URL, nil
}

// List lists objects with the given prefix. Returns keys.
func (d *S3Driver) List(prefix string) ([]string, error) {
	out, err := d.client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(d.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: list %q: %w", prefix, err)
	}

	keys := make([]string, 0, len(out.Contents))
	for _, obj := range out.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}
	return keys, nil
}
