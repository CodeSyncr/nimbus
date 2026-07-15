// Package cashier is a multi-gateway billing plugin for Nimbus, modeled on
// Laravel Cashier but gateway-agnostic. One app can register several payment
// gateways (Stripe, Razorpay, PayU, …), pick a default, select a gateway per
// request, verify webhooks, and gate access with a paywall.
//
//	app.Use(cashier.NewPlugin(cashier.Config{
//	    FromEnv:  true,          // register gateways whose env keys are set
//	    Default:  "razorpay",    // default gateway
//	    OnWebhook: func(e cashier.WebhookEvent) error {
//	        switch events.Normalize(e) {
//	        case events.PaymentSucceeded:      // grant paywall access
//	        case events.SubscriptionCancelled: // revoke
//	        }
//	        return nil
//	    },
//	}))
//
// Layout:
//
//	cashier.go   – plugin + facade      config.go   – Config
//	manager.go   – GatewayManager       paywall.go  – Paywall engine
//	routes.go    – webhook routes       contracts/  – PaymentGateway interface
//	gateways/    – Stripe/Razorpay/PayU  models/     – transactions, subscriptions
//	events/      – canonical events     views/      – checkout templates
package cashier

import (
	"context"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
	"github.com/CodeSyncr/nimbus/plugins/cashier/gateways"
	"github.com/CodeSyncr/nimbus/plugins/cashier/models"
)

// Re-exported contract types so callers use the cashier package without
// importing contracts directly.
type (
	Gateway      = contracts.PaymentGateway
	Charge       = contracts.Charge
	ChargeParams = contracts.ChargeParams
	PaymentProof = contracts.PaymentProof
	WebhookEvent = contracts.WebhookEvent
)

// Cashier is the facade tying the gateway manager to the paywall. Resolve it
// from the container ("cashier") or hold the plugin's instance.
type Cashier struct {
	Gateways *GatewayManager
	Paywall  *Paywall
}

// Charge starts a payment on the named gateway (empty → the default gateway).
func (c *Cashier) Charge(ctx context.Context, gatewayName string, p ChargeParams) (*Charge, error) {
	gw, err := c.Gateways.Use(gatewayName)
	if err != nil {
		return nil, err
	}
	return gw.CreateCharge(ctx, p)
}

// HasAccess is a shortcut to the paywall check.
func (c *Cashier) HasAccess(subject, plan string) bool { return c.Paywall.HasAccess(subject, plan) }

// ── Plugin ────────────────────────────────────────────────────────

var (
	_ nimbus.Plugin        = (*Plugin)(nil)
	_ nimbus.HasRoutes     = (*Plugin)(nil)
	_ nimbus.HasConfig     = (*Plugin)(nil)
	_ nimbus.HasMigrations = (*Plugin)(nil)
)

// Plugin wires Cashier into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	Cashier *Cashier
	cfg     Config
}

// NewPlugin builds the Cashier plugin from config.
func NewPlugin(cfg Config) *Plugin {
	if cfg.Manager == nil {
		cfg.Manager = NewGatewayManager()
	}
	if cfg.Paywall == nil {
		cfg.Paywall = NewPaywall(nil)
	}
	if cfg.WebhookPrefix == "" {
		cfg.WebhookPrefix = "/payments"
	}
	if cfg.FromEnv {
		registerFromEnv(cfg.Manager)
	}
	def := cfg.Default
	if def == "" {
		def = os.Getenv("PAYMENTS_DEFAULT_GATEWAY")
	}
	if def != "" {
		cfg.Manager.SetDefault(def)
	}
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{PluginName: "cashier", PluginVersion: "1.0.0"},
		Cashier:    &Cashier{Gateways: cfg.Manager, Paywall: cfg.Paywall},
		cfg:        cfg,
	}
}

// registerFromEnv adds gateways whose credentials are present in the environment.
func registerFromEnv(m *GatewayManager) {
	if k := os.Getenv("STRIPE_KEY"); k != "" {
		m.Register(gateways.NewStripe(gateways.StripeConfig{
			SecretKey:     k,
			WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		}))
	}
	if id := os.Getenv("RAZORPAY_KEY_ID"); id != "" {
		m.Register(gateways.NewRazorpay(gateways.RazorpayConfig{
			KeyID:         id,
			KeySecret:     os.Getenv("RAZORPAY_KEY_SECRET"),
			WebhookSecret: os.Getenv("RAZORPAY_WEBHOOK_SECRET"),
		}))
	}
	if k := os.Getenv("PAYU_MERCHANT_KEY"); k != "" {
		m.Register(gateways.NewPayU(gateways.PayUConfig{
			MerchantKey:  k,
			MerchantSalt: os.Getenv("PAYU_MERCHANT_SALT"),
			PaymentURL:   os.Getenv("PAYU_PAYMENT_URL"),
		}))
	}
}

func (p *Plugin) Register(app *nimbus.App) error {
	app.Container.Singleton("cashier", func() *Cashier { return p.Cashier })
	app.Container.Singleton("cashier.manager", func() *GatewayManager { return p.Cashier.Gateways })
	app.Container.Singleton("cashier.paywall", func() *Paywall { return p.Cashier.Paywall })
	return nil
}

func (p *Plugin) Boot(app *nimbus.App) error { return nil }

// Migrations creates the cashier tables (transactions, subscriptions).
func (p *Plugin) Migrations() []database.Migration { return models.Migrations() }

func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"default_gateway": p.Cashier.Gateways.DefaultName(),
		"gateways":        p.Cashier.Gateways.Names(),
		"webhook_prefix":  p.cfg.WebhookPrefix,
	}
}
