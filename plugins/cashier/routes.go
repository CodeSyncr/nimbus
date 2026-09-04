package cashier

import (
	"io"
	stdhttp "net/http"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RegisterRoutes mounts a signature-verified webhook endpoint per gateway at
// "<WebhookPrefix>/<gateway>/webhook" (e.g. /payments/razorpay/webhook).
func (p *Plugin) RegisterRoutes(r *router.Router) {
	for _, name := range p.Cashier.Gateways.Names() {
		gwName := name // capture
		r.Post(p.cfg.WebhookPrefix+"/"+gwName+"/webhook", p.webhookHandler(gwName))
	}

	// The current offering — what a paywall should present right now — with
	// each package's product resolved. Only mounted when the Cashier Cloud
	// suite is active and offerings exist: a catalogue is public pricing,
	// but an app that has none should not expose an empty endpoint.
	if p.Cashier.Catalog != nil {
		if _, ok := p.Cashier.Catalog.CurrentOffering(); ok {
			r.Get(p.cfg.WebhookPrefix+"/offerings", p.offeringsHandler())
		}
	}
}

func (p *Plugin) offeringsHandler() router.HandlerFunc {
	return func(c *nhttp.Context) error {
		current, _ := p.Cashier.Catalog.CurrentOffering()
		type pkg struct {
			ID      string  `json:"id"`
			Product Product `json:"product"`
		}
		packages := make([]pkg, 0, len(current.Packages))
		for _, pk := range current.Packages {
			prod, ok := p.Cashier.Catalog.Product(pk.ProductID)
			if !ok {
				prod = Product{ID: pk.ProductID}
			}
			packages = append(packages, pkg{ID: pk.ID, Product: prod})
		}
		return c.JSON(stdhttp.StatusOK, map[string]any{
			"current_offering": current.ID,
			"metadata":         current.Metadata,
			"packages":         packages,
		})
	}
}

func (p *Plugin) webhookHandler(gwName string) router.HandlerFunc {
	return func(c *nhttp.Context) error {
		gw, err := p.Cashier.Gateways.Gateway(gwName)
		if err != nil {
			return c.JSON(stdhttp.StatusNotFound, map[string]string{"error": "gateway_not_found"})
		}
		// Read the RAW body — re-serialized JSON/form would break the signature.
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return c.JSON(stdhttp.StatusBadRequest, map[string]string{"error": "read_body"})
		}
		event, err := gw.VerifyWebhook(body, c.Request.Header)
		if err != nil {
			// 400 tells the gateway the delivery failed verification.
			return c.JSON(stdhttp.StatusBadRequest, map[string]string{"error": "invalid_signature"})
		}
		if p.cfg.OnWebhook != nil {
			if err := p.cfg.OnWebhook(*event); err != nil {
				// 500 so the gateway retries delivery.
				return c.JSON(stdhttp.StatusInternalServerError, map[string]string{"error": "handler_failed"})
			}
		}
		return c.JSON(stdhttp.StatusOK, map[string]string{"received": "true"})
	}
}
