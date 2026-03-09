/*
|--------------------------------------------------------------------------
| Drive Configuration
|--------------------------------------------------------------------------
|
| Configuration for the Drive plugin. Supports multiple disks: fs, s3, gcs,
| r2 (Cloudflare), spaces (DigitalOcean), supabase. DRIVE_DISK env var selects default.
|
| Environment variables per provider:
|
|   Local (fs):
|     DRIVE_DISK=fs
|
|   Amazon S3:
|     DRIVE_DISK=s3
|     AWS_ACCESS_KEY_ID=
|     AWS_SECRET_ACCESS_KEY=
|     AWS_REGION=us-east-1
|     S3_BUCKET=
|
|   Google Cloud Storage:
|     DRIVE_DISK=gcs
|     GCS_KEY=file://gcs_key.json
|     GCS_BUCKET=
|
|   Cloudflare R2:
|     DRIVE_DISK=r2
|     R2_KEY=
|     R2_SECRET=
|     R2_BUCKET=
|     R2_ENDPOINT=https://<account_id>.r2.cloudflarestorage.com
|
|   DigitalOcean Spaces:
|     DRIVE_DISK=spaces
|     SPACES_KEY=
|     SPACES_SECRET=
|     SPACES_REGION=nyc3
|     SPACES_BUCKET=
|     SPACES_ENDPOINT=https://<region>.digitaloceanspaces.com
|
|   Supabase Storage:
|     DRIVE_DISK=supabase
|     SUPABASE_STORAGE_KEY=
|     SUPABASE_STORAGE_SECRET=
|     SUPABASE_STORAGE_REGION=
|     SUPABASE_STORAGE_BUCKET=
|     SUPABASE_ENDPOINT=https://<project>.supabase.co/storage/v1/s3
|
*/

package drive

import "os"

// Config holds Drive plugin configuration.
type Config struct {
	// Default is the default disk name (fs, s3, gcs, r2, spaces, supabase).
	Default string

	// Disks holds per-disk configuration.
	Disks map[string]DiskConfig
}

// DiskConfig is the configuration for a single disk.
type DiskConfig struct {
	Driver     string     // "fs", "s3", "gcs"
	Visibility Visibility // "public" or "private"

	// FS driver
	Location      string // root directory for local storage
	ServeFiles    bool   // register route to serve files via HTTP
	RouteBasePath string // URL path prefix, e.g. "/uploads"

	// S3 driver (also used for R2, Spaces, Supabase - S3-compatible)
	S3Bucket         string
	S3Region         string
	S3AccessKey      string
	S3SecretKey      string
	S3Endpoint       string // for S3-compatible (MinIO, R2, Spaces)
	S3ForcePathStyle bool

	// GCS driver
	GCSBucket string
	GCSKey    string // path to service account JSON, e.g. file://gcs_key.json
}

// DefaultConfig returns default Drive configuration (fs only).
func DefaultConfig() Config {
	return Config{
		Default: "fs",
		Disks: map[string]DiskConfig{
			"fs": {
				Driver:        "fs",
				Visibility:    VisibilityPublic,
				Location:      "storage",
				ServeFiles:    true,
				RouteBasePath: "/uploads",
			},
		},
	}
}

// ConfigFromEnv builds Config from environment variables.
// Adds disks for each provider that has required env vars set.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if d := os.Getenv("DRIVE_DISK"); d != "" {
		cfg.Default = d
	}

	// S3
	if os.Getenv("S3_BUCKET") != "" {
		cfg.Disks["s3"] = DiskConfig{
			Driver:      "s3",
			Visibility:  VisibilityPublic,
			S3Bucket:    os.Getenv("S3_BUCKET"),
			S3Region:    getEnv("AWS_REGION", "us-east-1"),
			S3AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
			S3SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		}
	}

	// Cloudflare R2 (S3-compatible)
	if os.Getenv("R2_BUCKET") != "" {
		cfg.Disks["r2"] = DiskConfig{
			Driver:           "s3",
			Visibility:       VisibilityPublic,
			S3Bucket:         os.Getenv("R2_BUCKET"),
			S3Region:         "auto",
			S3AccessKey:      os.Getenv("R2_KEY"),
			S3SecretKey:      os.Getenv("R2_SECRET"),
			S3Endpoint:       os.Getenv("R2_ENDPOINT"),
			S3ForcePathStyle: true,
		}
	}

	// DigitalOcean Spaces (S3-compatible)
	if os.Getenv("SPACES_BUCKET") != "" {
		cfg.Disks["spaces"] = DiskConfig{
			Driver:           "s3",
			Visibility:       VisibilityPublic,
			S3Bucket:         os.Getenv("SPACES_BUCKET"),
			S3Region:         getEnv("SPACES_REGION", "nyc3"),
			S3AccessKey:      os.Getenv("SPACES_KEY"),
			S3SecretKey:      os.Getenv("SPACES_SECRET"),
			S3Endpoint:       os.Getenv("SPACES_ENDPOINT"),
			S3ForcePathStyle: true,
		}
	}

	// GCS
	if os.Getenv("GCS_BUCKET") != "" {
		cfg.Disks["gcs"] = DiskConfig{
			Driver:     "gcs",
			Visibility: VisibilityPublic,
			GCSBucket:  os.Getenv("GCS_BUCKET"),
			GCSKey:     os.Getenv("GCS_KEY"),
		}
	}

	// Supabase Storage (S3-compatible)
	if os.Getenv("SUPABASE_STORAGE_BUCKET") != "" {
		cfg.Disks["supabase"] = DiskConfig{
			Driver:           "s3",
			Visibility:       VisibilityPublic,
			S3Bucket:         os.Getenv("SUPABASE_STORAGE_BUCKET"),
			S3Region:         os.Getenv("SUPABASE_STORAGE_REGION"),
			S3AccessKey:      os.Getenv("SUPABASE_STORAGE_KEY"),
			S3SecretKey:      os.Getenv("SUPABASE_STORAGE_SECRET"),
			S3Endpoint:       os.Getenv("SUPABASE_ENDPOINT"),
			S3ForcePathStyle: true,
		}
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
