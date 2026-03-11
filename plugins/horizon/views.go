package horizon

import (
	"embed"
	"io/fs"
)

//go:embed views/*.nimbus
var viewsFS embed.FS

// ViewsFS returns the embedded Horizon views for the view engine.
func (p *Plugin) ViewsFS() fs.FS {
	f, _ := fs.Sub(viewsFS, "views")
	return f
}
