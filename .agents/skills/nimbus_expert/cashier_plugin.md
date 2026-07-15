# Cashier Plugin - Nimbus

The Cashier plugin (`plugins/cashier`) is a multi-gateway billing/payments layer for Nimbus, modeled on Laravel Cashier but gateway-agnostic. A single app can register several payment gateways, pick a default, route a charge to a specific gateway per request, verify webhooks with real signature checks, and gate access behind a paywall.

## Directory Layout

```
plugins/cashier/
  cashier.go       // facade (Cashier), Plugin, NewPlugin, type aliases, container bindings
  config.go        // Config{Manager, Default, FromEnv, Paywall, WebhookPrefix, OnWebhook}
  manager.go       // GatewayManager (Register/SetDefault/Default/Use/Names)
  paywall.go       // Paywall, EntitlementStore, MemoryEntitlementStore, RequirePlan
  routes.go        // RegisterRoutes → <WebhookPrefix>/<gateway>/webhook
  contracts/       // gateway.go: PaymentGateway interface + Charge/ChargeParams/PaymentProof/WebhookEvent
  gateways/        // stripe.go, razorpay.go, payu.go, helpers.go
  models/          // transaction.go, subscription.go, migrations.go
  events/          // events.go: canonical constants + Normalize()
  views/           // checkout.nimbus (Razorpay launcher)
```

## Installation

Register in `bin/server.go`. `FromEnv: true` auto-registers any gateway whose env credentials are present:

```go
import (
    "github.com/CodeSyncr/nimbus/plugins/cashier"
    "github.com/CodeSyncr/nimbus/plugins/cashier/events"
)

app.Use(cashier.NewPlugin(cashier.Config{
    FromEnv: true,
    Default: "razorpay",
    OnWebhook: func(e cashier.WebhookEvent) error {
        switch events.Normalize(e) {
        case events.PaymentSucceeded:      // grant paywall access
        case events.SubscriptionCancelled: // revoke access
        }
        return nil
    },
}))
```

The plugin runs migrations for `cashier_transactions` and `cashier_subscriptions`, and binds `cashier` (*Cashier facade), `cashier.manager` (*GatewayManager), and `cashier.paywall` (*Paywall) into the container.

## Gateways & Env Vars

| Gateway  | Region        | Env vars                                                              | Signature |
|----------|---------------|----------------------------------------------------------------------|-----------|
| Stripe   | International  | `STRIPE_KEY`, `STRIPE_WEBHOOK_SECRET`                                 | HMAC-SHA256 of `t.body`, `Stripe-Signature: t=…,v1=…`, timestamp tolerance |
| Razorpay | India         | `RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`, `RAZORPAY_WEBHOOK_SECRET`   | payment sig = HMAC-SHA256(`order|payment`, secret); webhook = HMAC-SHA256(body, whsecret) in `X-Razorpay-Signature`. Amounts in paise |
| PayU     | India         | `PAYU_MERCHANT_KEY`, `PAYU_MERCHANT_SALT`, `PAYU_PAYMENT_URL`         | SHA-512 hash; request `key|txnid|amount|productinfo|firstname|email|udf1..10|salt`; response = reverse. Amounts in rupees (Amount/100) |

Default gateway resolution: `Config.Default` → `PAYMENTS_DEFAULT_GATEWAY` env → first registered.

Register explicitly (instead of `FromEnv`) for custom config:

```go
m := cashier.NewGatewayManager()
m.Register(gateways.NewRazorpay(gateways.RazorpayConfig{KeyID: id, KeySecret: secret, WebhookSecret: whs}))
m.Register(gateways.NewStripe(gateways.StripeConfig{SecretKey: sk, WebhookSecret: whs}))
m.SetDefault("razorpay")
app.Use(cashier.NewPlugin(cashier.Config{Manager: m}))
```

## Creating a Charge

```go
cash := app.Container.MustMake("cashier").(*cashier.Cashier)

// "" → default gateway; amount in smallest unit (paise/cents).
charge, err := cash.Charge(ctx, "", cashier.ChargeParams{
    Amount: 49900, Currency: "INR", Reference: "order_123", CustomerEmail: "u@x.com",
})

// Named gateway for an international customer:
charge, err = cash.Charge(ctx, "stripe", cashier.ChargeParams{
    Mode: "subscription", PriceID: "price_abc",
    SuccessURL: "https://app/ok", CancelURL: "https://app/cancel",
})
```

Stripe returns `charge.RedirectURL` (redirect browser). Razorpay/PayU return an order/txn id the frontend opens via `views/checkout.nimbus`.

## Webhooks

The plugin mounts a signature-verified endpoint per gateway at `<WebhookPrefix>/<gateway>/webhook` (default prefix `/payments`). `routes.go` reads the RAW body for signature verification, each gateway's `VerifyWebhook` checks its own signature, then `OnWebhook` fires. Use `events.Normalize(evt)` to branch on canonical events (`PaymentSucceeded`, `PaymentFailed`, `SubscriptionActivated`, `SubscriptionCancelled`, `Unknown`) across all gateways.

## Paywall

```go
pw := app.Container.MustMake("cashier.paywall").(*cashier.Paywall)

pw.Grant(userID, "pro", time.Now().Add(30*24*time.Hour)) // zero time = forever
pw.HasAccess(userID, "pro")
pw.Revoke(userID, "pro")

// Middleware — returns HTTP 402 Payment Required when denied:
subject := func(c *http.Context) string { return currentUserID(c) }
app.Router.Group("/pro", pw.RequirePlan("pro", subject)).Get("/report", handler)
```

Default store is `MemoryEntitlementStore`; implement `EntitlementStore` for a DB-backed paywall.

## Adding a Gateway

Add a file in `gateways/` implementing `contracts.PaymentGateway` (`Name`, `CreateCharge`, `VerifyPayment`, `VerifyWebhook`) and register it in the manager. PhonePe, Cashfree, Paytm, PayPal slot in the same way. A gateway that can't verify a bare payment (like PayU) returns `contracts.ErrUnsupported` from `VerifyPayment` and validates via `VerifyWebhook` with the full callback form.
