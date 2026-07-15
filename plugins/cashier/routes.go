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
