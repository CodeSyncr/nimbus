# Drive Plugin

The Drive plugin provides a unified file storage abstraction for Nimbus applications, supporting multiple cloud storage providers.

## Installation
```bash
nimbus plugin install drive
```

## Configuration
```go
// config/drive.go
drive.Config{
    Default: "local",
    Disks: map[string]drive.DiskConfig{
        "local": {Driver: "local", Root: "./storage/app"},
        "s3":    {Driver: "s3", Bucket: "my-bucket", Region: "us-east-1"},
    },
}
```

## Supported Drivers
| Driver | Package | Description |
|--------|---------|-------------|
| `local` | Built-in | Local filesystem |
| `s3` | Built-in | Amazon S3 |
| `gcs` | Built-in | Google Cloud Storage |
| `r2` | Built-in | Cloudflare R2 |
| `spaces` | Built-in | DigitalOcean Spaces |
| `supabase` | Built-in | Supabase Storage |

## Usage

### Basic Operations
```go
// Put a file
err := drive.Put("avatars/user-1.jpg", imageData)

// Get a file
data, err := drive.Get("avatars/user-1.jpg")

// Check existence
exists := drive.Exists("avatars/user-1.jpg")

// Delete a file
err := drive.Delete("avatars/user-1.jpg")
```

### Using Named Disks
```go
// Use a specific disk
s3 := drive.Disk("s3")
err := s3.Put("backups/db.sql.gz", data)
```

### File Uploads (with HTTP Context)
```go
func Upload(c *http.Context) error {
    file, header, err := c.Request.FormFile("avatar")
    if err != nil {
        return c.JSON(400, map[string]string{"error": "no file"})
    }
    defer file.Close()
    
    data, _ := io.ReadAll(file)
    path := "avatars/" + header.Filename
    if err := drive.Put(path, data); err != nil {
        return c.JSON(500, map[string]string{"error": "upload failed"})
    }
    return c.JSON(200, map[string]string{"path": path})
}
```

## Plugin Interface
Drive implements `HasConfig`, `HasBindings`, and `HasRoutes` capabilities.
