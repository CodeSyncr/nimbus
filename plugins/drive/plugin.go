/*
|--------------------------------------------------------------------------
| Drive Plugin for Nimbus
|--------------------------------------------------------------------------
|
| Drive provides a unified API for file storage across local filesystem,
| S3, and GCS. Inspired by AdonisJS Drive.
|
| Usage:
|
|   app.Use(drive.New(drive.Config{
|       Default: "fs",
|       Disks: map[string]drive.DiskConfig{
|           "fs": {Driver: "fs", Location: "storage", ServeFiles: true, RouteBasePath: "/uploads"},
|           "s3": {Driver: "s3", S3Bucket: "my-bucket", ...},
|       },
|   }))
|
|   // In a handler
|   disk, _ := drive.Use("")
|   disk.Put("avatars/1.jpg", file)
|   url, _ := disk.GetUrl("avatars/1.jpg")
|
|   // Access at /uploads/avatars/1.jpg when using fs with ServeFiles
|
*/

package drive

import (
	"io"
	"net/http"
	"os"
	"strings"

	reqctx "github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus"
)

var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasRoutes = (*Plugin)(nil)
)

// Plugin integrates Drive file storage into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	config  Config
	manager *Manager
}

// New creates a new Drive plugin. Pass nil to use ConfigFromEnv (env-based disks).
func New(cfg *Config) *Plugin {
	var config Config
	if cfg != nil {
		config = *cfg
		if d := os.Getenv("DRIVE_DISK"); d != "" {
			config.Default = d
		}
	} else {
		config = ConfigFromEnv()
	}
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "drive",
			PluginVersion: "1.0.0",
		},
		config: config,
	}
}

// Register binds the Drive manager.
func (p *Plugin) Register(app *nimbus.App) error {
	return nil
}

// Boot initializes the Drive manager and sets it globally.
func (p *Plugin) Boot(app *nimbus.App) error {
	p.manager = NewManager(p.config)
	SetGlobal(p.manager)
	return nil
}

// RegisterRoutes mounts the file serving route when ServeFiles is true.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	for name, dc := range p.config.Disks {
		if dc.Driver != "fs" || !dc.ServeFiles || dc.RouteBasePath == "" {
			continue
		}
		basePath := strings.TrimSuffix(dc.RouteBasePath, "/")
		pattern := basePath + "/*"
		disk, err := p.manager.Use(name)
		if err != nil {
			continue
		}
		fsDisk, ok := disk.(*FSDisk)
		if !ok {
			continue
		}
		r.Get(pattern, p.serveHandler(fsDisk, basePath))
		break // only one fs serve route
	}
}

func (p *Plugin) serveHandler(d *FSDisk, basePath string) router.HandlerFunc {
	return func(c *reqctx.Context) error {
		path := strings.TrimPrefix(c.Request.URL.Path, basePath+"/")
		path, err := SafePath(path)
		if err != nil {
			c.Response.WriteHeader(http.StatusNotFound)
			return nil
		}
		rc, err := d.Get(path)
		if err != nil {
			if os.IsNotExist(err) {
				c.Response.WriteHeader(http.StatusNotFound)
				return nil
			}
			return err
		}
		defer rc.Close()
		c.Response.Header().Set("Content-Type", ContentType(path))
		_, err = io.Copy(c.Response, rc)
		return err
	}
}
