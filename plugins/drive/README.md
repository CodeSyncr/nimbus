# Drive Plugin for Nimbus

Unified file storage across local filesystem, S3, GCS, R2, Spaces, and Supabase. Inspired by [AdonisJS Drive](https://docs.adonisjs.com/guides/drive).

## Installation

Drive is a default plugin when creating a new app with `nimbus new`. To add manually:

```bash
nimbus add drive
```

Or in `bin/server.go`:

```go
import "github.com/CodeSyncr/nimbus/plugins/drive"

app.Use(drive.New(nil))  // nil = ConfigFromEnv()
```

## Configuration

### Environment-based (recommended)

Set `DRIVE_DISK` and provider-specific vars in `.env`:

| Variable | Description | Default |
|----------|-------------|---------|
| `DRIVE_DISK` | Default disk: `fs`, `s3`, `gcs`, `r2`, `spaces`, `supabase` | `fs` |

**Local (fs):** No extra vars. Uses `storage/` by default.

**Amazon S3:**
```
DRIVE_DISK=s3
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=
AWS_REGION=us-east-1
S3_BUCKET=
```

**Cloudflare R2:**
```
DRIVE_DISK=r2
R2_KEY=
R2_SECRET=
R2_BUCKET=
R2_ENDPOINT=https://<account_id>.r2.cloudflarestorage.com
```

**DigitalOcean Spaces:**
```
DRIVE_DISK=spaces
SPACES_KEY=
SPACES_SECRET=
SPACES_REGION=nyc3
SPACES_BUCKET=
SPACES_ENDPOINT=https://<region>.digitaloceanspaces.com
```

**Google Cloud Storage:**
```
DRIVE_DISK=gcs
GCS_BUCKET=
GCS_KEY=file://path/to/service-account.json
```

**Supabase Storage:**
```
DRIVE_DISK=supabase
SUPABASE_STORAGE_KEY=
SUPABASE_STORAGE_SECRET=
SUPABASE_STORAGE_REGION=
SUPABASE_STORAGE_BUCKET=
SUPABASE_ENDPOINT=https://<project>.supabase.co/storage/v1/s3
```

### Code-based config

```go
app.Use(drive.New(&drive.Config{
    Default: "fs",
    Disks: map[string]drive.DiskConfig{
        "fs": {
            Driver:        "fs",
            Location:      "storage",
            ServeFiles:    true,
            RouteBasePath: "/uploads",
        },
        "s3": {
            Driver:      "s3",
            S3Bucket:    "my-bucket",
            S3Region:    "us-east-1",
            S3AccessKey: "...",
            S3SecretKey: "...",
        },
    },
}))
```

## Usage

```go
import "github.com/CodeSyncr/nimbus/plugins/drive"

func uploadHandler(c *context.Context) error {
    disk, err := drive.Use("")  // default disk
    if err != nil {
        return err
    }

    file, _, _ := c.Request.FormFile("avatar")
    defer file.Close()

    err = disk.Put("avatars/1.jpg", file)
    if err != nil {
        return err
    }

    url, _ := disk.GetUrl("avatars/1.jpg")
    return c.JSON(200, map[string]string{"url": url})
}
```

### Disk operations

| Method | Description |
|--------|-------------|
| `disk.Put(path, reader)` | Write file |
| `disk.Get(path)` | Read file (returns `io.ReadCloser`) |
| `disk.Delete(path)` | Remove file |
| `disk.Exists(path)` | Check existence |
| `disk.GetUrl(path)` | Public URL |
| `disk.GetSignedUrl(path, expiresIn)` | Temporary signed URL (private) |

### Serving local files

When using the `fs` driver with `ServeFiles: true` and `RouteBasePath: "/uploads"`, files are served at `/uploads/*`. Example: `disk.GetUrl("avatars/1.jpg")` → `/uploads/avatars/1.jpg`.
