/*
|--------------------------------------------------------------------------
| Docs Plugin for Nimbus
|--------------------------------------------------------------------------
|
| Wires the Nimbus Cloud document APIs (Sigma spreadsheets and Carbon
| documents) into a Nimbus app. Keys come from the environment; clients
| are registered in the container and exposed through package facades.
|
|   // bin/server.go
|   app.Use(docs.New())
|
|   // anywhere
|   doc, err := docs.Sigma().Upload(ctx, nimbusdocs.Upload{...})
|
| Env:
|   SIGMA_API_KEY   sgm_live_…   enables docs.Sigma()
|   CARBON_API_KEY  cbn_live_…   enables docs.Carbon()
|   NIMBUS_DOCS_URL              override the API origin (local testing)
|
*/

package docs

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/packages/nimbusdocs"
)

var _ nimbus.Plugin = (*Plugin)(nil)

// Plugin registers document clients for whichever products have keys.
type Plugin struct {
	nimbus.BasePlugin
}

// New creates the plugin.
func New() *Plugin {
	return &Plugin{BasePlugin: nimbus.BasePlugin{PluginName: "docs", PluginVersion: "1.0.0"}}
}

var (
	mu      sync.RWMutex
	clients = map[nimbusdocs.Product]*nimbusdocs.Client{}
)

// Register builds a client per configured key.
func (p *Plugin) Register(app *nimbus.App) error {
	var opts []nimbusdocs.Option
	if u := strings.TrimSpace(os.Getenv("NIMBUS_DOCS_URL")); u != "" {
		opts = append(opts, nimbusdocs.WithBaseURL(u))
	}
	for product, env := range map[nimbusdocs.Product]string{
		nimbusdocs.Sigma:  "SIGMA_API_KEY",
		nimbusdocs.Carbon: "CARBON_API_KEY",
	} {
		key := strings.TrimSpace(os.Getenv(env))
		if key == "" {
			continue
		}
		c, err := nimbusdocs.New(product, key, opts...)
		if err != nil {
			return fmt.Errorf("docs: %s: %w", env, err)
		}
		set(product, c)
		name := "docs." + string(product)
		app.Container.Singleton(name, func() *nimbusdocs.Client { return c })
	}
	return nil
}

// Boot is a no-op.
func (p *Plugin) Boot(app *nimbus.App) error { return nil }

func set(product nimbusdocs.Product, c *nimbusdocs.Client) {
	mu.Lock()
	defer mu.Unlock()
	clients[product] = c
}

// For returns the client for a product, or nil when its key is not set.
func For(product nimbusdocs.Product) *nimbusdocs.Client {
	mu.RLock()
	defer mu.RUnlock()
	return clients[product]
}

// Sigma is the spreadsheet client, or nil when SIGMA_API_KEY is unset.
func Sigma() *nimbusdocs.Client { return For(nimbusdocs.Sigma) }

// Carbon is the document client, or nil when CARBON_API_KEY is unset.
func Carbon() *nimbusdocs.Client { return For(nimbusdocs.Carbon) }
