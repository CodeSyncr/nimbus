# Storage & File Uploads

The `storage` package handles uploaded files, pluggable drivers, and signed
temporary URLs. (The richer cloud-storage abstraction is the
[Drive plugin](drive_plugin.md); `storage` is the layer beneath it.)

## Drivers

`Driver` is an interface: `Put(path, io.Reader) error`, `Get(path) (io.ReadCloser, error)`,
`Delete(path) error`, `Exists(path) (bool, error)`.

```go
local := storage.NewLocalDriver("storage/app")
s3    := storage.NewS3Driver(storage.S3Config{ ... })
```

## Handling an upload

```go
func Upload(c *http.Context) error {
    _, fh, err := c.File("avatar")
    if err != nil {
        return c.BadRequest("no file")
    }

    up := storage.NewUploadedFile(fh)
    if !up.IsValid() {
        return c.BadRequest("invalid upload")
    }
    if !storage.AllowedExtensions(up, "jpg", "jpeg", "png", "webp") {
        return c.BadRequest("unsupported type")
    }
    if !storage.MaxFileSize(up, 5<<20) {  // 5 MB
        return c.BadRequest("too large")
    }

    path, err := up.StoreRandomName(local, "avatars")
    if err != nil {
        return err
    }
    return c.JSON(200, map[string]string{"path": path})
}
```

### `UploadedFile`

| Method | Returns |
| --- | --- |
| `Open() (multipart.File, error)` | The reader |
| `Name() string` | Original filename |
| `Size() int64` | Bytes |
| `Extension() string` | File extension |
| `MimeType() (string, error)` | Detected content type |
| `IsValid() bool` | Basic sanity check |
| `Store(driver, dir) (string, error)` | Save, keeping the original name |
| `StoreAs(driver, dir, name) (string, error)` | Save under a given name |
| `StoreRandomName(driver, dir) (string, error)` | Save under a generated name |

**Prefer `StoreRandomName`.** A client-supplied filename is attacker-controlled:
it can carry path separators, collide with an existing file, or end in an
extension your web server will execute.

Validate the **detected** `MimeType()`, not just the extension — the two do not
have to agree.

### One-liners

```go
path, err := storage.PutFromRequest(r, "avatar", driver, "avatars")
path, err := storage.PutFromRequestAs(r, "avatar", driver, "avatars", "me.png")
```

## Signed temporary URLs

Serve private files without making them public:

```go
gen := storage.NewSignedURLGenerator(secret, "https://app.example.com")
url := gen.TemporaryURL("avatars/abc.png", 15*time.Minute)

app.Router.Mount("/files/", storage.ServeSignedFiles(driver, gen, "/files/"))
```

`Verify(path, signature, expires) bool` is the check `ServeSignedFiles` performs
for you. Use a long random secret — the signature is the only thing standing
between a guessed path and the file.

## Guidance

1. Never store uploads inside `public/` — that is the directory the server hands
   out without asking anyone.
2. Enforce size limits at two layers: `middleware.BodyLimit` for the request and
   `MaxFileSize` for the file.
3. Keep the storage path in the database, not the URL, so you can move drivers
   without a data migration.
