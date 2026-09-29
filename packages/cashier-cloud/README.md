# @codesyncr/cashier-cloud

The web SDK for **Cashier Cloud** — one subscription state across Apple, Google
and Stripe, read from the browser.

This package is the client and nothing else. It carries a **public** key, so
everything it can do is safe to ship in a page: read what the paywall should
sell, read what the current subscriber owns, and start a checkout. Receipt
verification, metering and the entitlement ledger run on Cloud, because a
credential that ships to a browser is not a credential.

```bash
npm install @codesyncr/cashier-cloud
```

Works in any browser bundler and in Node 18+ (SSR-safe — it falls back to
in-memory storage where `localStorage` is absent).

---

## Quick start

```typescript
import { CashierCloud } from '@codesyncr/cashier-cloud'

const cashier = new CashierCloud({ apiKey: 'cshr_pub_…' })

// 1. What should the paywall present?
const { packages } = await cashier.offerings()

// 2. Send them to pay.
await cashier.purchase(packages[0], {
  successUrl: 'https://example.com/welcome',
  cancelUrl: 'https://example.com/pricing',
})

// 3. On the success page, refetch and unlock.
const info = await cashier.customerInfo({ force: true })
if (info.entitlements.premium?.active) unlockPro()
```

---

## Configuration

Configure once for the page:

```typescript
import { CashierCloud } from '@codesyncr/cashier-cloud'

CashierCloud.configure({ apiKey: 'cshr_pub_…', appUserId: user?.id })

// anywhere else
const cashier = CashierCloud.getSharedInstance()
```

`new CashierCloud(…)` works too, but identity and the CustomerInfo cache live on
the instance — two of them would disagree about who is signed in.

| Option | Type | Default |
|---|---|---|
| `apiKey` | `string` | — **required** |
| `appUserId` | `string` | a persisted anonymous id |
| `headers` | `Record<string, string>` | `{}` |
| `fetch` | `FetchLike` | global `fetch` |
| `storage` | `Storage` | `localStorage`, else memory |

**Only ever pass the public key.** It is scoped to reading this subscriber and
starting a checkout. The secret `cshr_live_…` key can grant entitlements and belongs on
your own server, never in a page.

**There is no `baseURL`.** The API origin is fixed at `https://nimbusgo.space`,
because the public key is scoped to that host — a page that could repoint the
SDK could hand the key, and every subscriber read made with it, to whoever owns
the other host. Tests and proxies inject `fetch`, which is narrower and
explicit.

---

## Identity

A person often buys **before** they sign up, and that purchase has to survive the
gap. So a subscriber always has an id: either yours, or an anonymous one the SDK
generates and persists.

```typescript
const cashier = new CashierCloud({ apiKey })
cashier.appUserId    // "$anon:9f2c…"  — generated once, kept in localStorage
cashier.isAnonymous  // true

await cashier.purchase(pkg)      // bought while anonymous
await cashier.logIn(user.id)     // merges that purchase into the real account
cashier.isAnonymous              // false
```

### `logIn(appUserId)`

Aliases the current subject onto your user id and returns the merged
`CustomerInfo`. Idempotent — calling it again for the same user is a no-op, not
a second alias. Once the merge lands the anonymous id is forgotten, because
keeping it would resurrect an empty subscriber on the next visit.

Call it as soon as you know who someone is — after sign-in, and on every app
load for an already-signed-in user.

### `logOut()`

Rotates to a fresh anonymous subscriber. Entitlements stay with the account they
were bought under; this only changes who *this browser* is reading as. Call it
when your own session ends.

---

## Reading subscriber state

### `customerInfo(options?)`

The one aggregate answering *what does this person have right now*.

```typescript
const info = await cashier.customerInfo()

info.entitlements.premium?.active      // boolean
info.entitlements.premium?.willRenew   // false once cancelled
info.entitlements.premium?.periodType  // trial | intro | normal | promotional | grace
info.entitlements.premium?.expiresAt   // Date | null  (null = never expires)
info.activeEntitlementIds              // ["premium"]
info.activeProductIds                  // ["pro_monthly"]
info.activeSubscriptions               // store subscriptions currently granting
info.latestExpiresAt                   // Date | null
```

Cached after the first call. Pass `{ force: true }` to refetch — do that when
you land on your success URL, and after `logIn`.

Two behaviours are worth knowing because they will otherwise look like bugs:

- **A cancelled subscription is still active.** Cancelling sets `willRenew` to
  `false`; access continues until `expiresAt`. Do not gate on `willRenew`.
- **A failed payment does not revoke immediately.** The entitlement enters a
  grace period — `active` stays `true` and `periodType` becomes `'grace'` — while
  the store retries. If you want to nudge the customer, that flag is the signal.

### `isEntitled(id)`

A synchronous read of the cached `CustomerInfo`. Convenient for rendering:

```typescript
if (cashier.isEntitled('premium')) showProBadge()
```

It is only as fresh as the last fetch, and it runs in a browser the user
controls. **Never gate anything that matters on it** — have your own server ask
Cloud instead.

### `onCustomerInfoUpdate(listener)`

Fires on every refresh. Returns an unsubscribe function.

```typescript
const off = cashier.onCustomerInfoUpdate((info) => {
  setPro(info.entitlements.premium?.active ?? false)
})
```

### `current` and `invalidate()`

`current` is the cached `CustomerInfo` or `null` before the first fetch.
`invalidate()` drops the cache so the next read refetches.

---

## The paywall

### `offerings()`

Returns the current offering with every package's product resolved, so a paywall
asks *what should I sell right now* instead of hard-coding product ids — which
means you can reprice or swap plans from the Cloud dashboard without shipping
a release.

```typescript
const { currentOffering, packages, metadata } = await cashier.offerings()

packages.forEach((pkg) => {
  pkg.id                    // "monthly" | "annual"
  pkg.product.id            // "pro_monthly"
  pkg.product.name          // "Pro"
  pkg.product.amount        // 149900 — smallest unit, always
  pkg.product.currency      // "INR"
  pkg.product.periodMonths  // 1  (0 = lifetime / non-renewing)
  pkg.product.trialDays     // 7
  pkg.product.entitlements  // ["premium"]
})
```

`packages()` is a shortcut when you only need the list. For the common cases,
the offering is addressable by billing period rather than by index — derived
from each product's `periodMonths`, so a package called `starter` still resolves:

```typescript
const { monthly, annual, lifetime } = await cashier.offerings()
```

Prices are the ones set on each product in the console, in that product's
currency.

> **Amounts are in the smallest currency unit** — paise, cents. `149900` is
> ₹1,499.00. Format with `Intl.NumberFormat`, dividing by 100.

### Taking web payments

Cashier hosts the checkout on **your own Stripe account**. Connect it once in
the console — your app → **Settings → Web payments · Stripe**: paste a secret key
(`sk_live_…`, or a restricted `rk_…` key with write access to Checkout Sessions
and read access to Subscriptions, Invoices and Charges), add the webhook URL shown
there as an endpoint in Stripe (events `checkout.session.completed`,
`invoice.paid`, `invoice.payment_failed`, `customer.subscription.updated`,
`customer.subscription.deleted`, `charge.refunded`), and paste that endpoint's
signing secret (`whsec_…`).

Then `purchase()` sends the buyer to Stripe Checkout, priced from the Cashier
product — a subscription with the product's period and free trial, or a
one-time payment for a lifetime product:

```typescript
const { annual } = await cashier.offerings()

await cashier.purchase(annual, {               // a package, a product, or a product id
  successUrl: 'https://example.com/welcome',   // default: the current page
  cancelUrl: 'https://example.com/pricing',    // default: the current page
})
// The browser is now on checkout.stripe.com. purchase() resolves with { url };
// pass { redirect: false } (or call it outside a browser) to only get the URL.
```

When the buyer pays, Stripe tells Cloud and the entitlement is granted to the
app user id that started the checkout — renewals, cancellations, failed
payments and refunds follow the same way, as the same events and revenue as
App Store and Play purchases. On the success page:

```typescript
const info = await cashier.customerInfo({ force: true })
if (info.entitlements.premium?.active) unlockPro()
```

The webhook can land a second or two after the redirect; if the entitlement is
not there yet, poll `customerInfo({ force: true })` briefly.

`presentPaywall()` does all of this by itself: without `onPurchase`, its
purchase button calls `purchase(pkg)` with the current page as both return
URLs (override with `successUrl` / `cancelUrl`; failures go to `onError`).

If the app has not connected Stripe, `purchase()` rejects with
`checkout_not_configured`:

```typescript
try {
  await cashier.purchase(pkg)
} catch (err) {
  if (err instanceof CashierCloudError && err.code === CashierErrorCode.CheckoutNotConfigured) {
    // connect Stripe in the Cashier console first
  }
}
```

**Your own checkout instead.** Pass `onPurchase` to `presentPaywall()` and take
the payment yourself; when your provider confirms the charge, **your server**
records it with the secret key:

```sh
curl -X POST https://nimbusgo.space/api/cashier/v1/purchases \
  -H "Authorization: Bearer $CASHIER_SECRET_KEY" \
  -H "Content-Type: application/json" \
  -d '{"app_user_id":"user_42","product_id":"pro_monthly"}'
```

### Languages, answers and experiments

The paywall you design in the console can carry translations, questions and
an A/B test; `presentPaywall()` handles all three:

```typescript
await cashier.presentPaywall({
  locale: 'es-MX',                 // default: navigator.language
  onAnswer: ({ screen_id, question, answer }) => {
    if (question === 'Marketing emails' && answer === 'yes') subscribeToNewsletter()
  },
})
```

- **Languages.** The paywall shows the translation that matches `locale`
  exactly, else one in the same language (`es-MX` → `es`), else the texts as
  written. `localizePaywall(doc, locale)` does the same for your own renderer.
- **Answers.** Buttons on Feedback and Marketing Consent screens can carry an
  answer. A tap calls `onAnswer`, and Cashier records it (Responses in the
  console, a `paywall_response` webhook event).
- **Experiments.** `offerings()` sends the app user id, so a customer in a
  running experiment gets their variant's offering and paywall;
  `offerings().experiment` says which (`{ id, variant: 'a' | 'b' }`). The
  paywall reports when it is shown and dismissed, which is what the console
  compares.

## Errors

Every failure throws `CashierCloudError` carrying a stable `code`. **Branch on
the code, not the status or the message** — statuses get reused and messages get
reworded; neither is a contract.

```typescript
import { CashierCloudError, CashierErrorCode } from '@codesyncr/cashier-cloud'

try {
  await cashier.purchase(pkg)
} catch (err) {
  if (!(err instanceof CashierCloudError)) throw err

  switch (err.code) {
    case CashierErrorCode.UserCancelled:  break                    // not worth surfacing
    case CashierErrorCode.Network:        return showRetry()
    case CashierErrorCode.PaymentRequired: return showUpgrade()
    default:                              return showGenericError(err.message)
  }
}
```

| Code | When |
|---|---|
| `network_error` | The request never reached Cloud. `status` is `0` |
| `invalid_api_key` | Missing, malformed, revoked, or a secret key in a browser |
| `invalid_app_user_id` | Empty or reserved app user id |
| `offering_not_found` | No offering configured, or the named one is gone |
| `product_not_available` | Not purchasable right now |
| `subscriber_not_found` | Never seen this subscriber |
| `payment_required` | The account is over its plan |
| `user_cancelled` | The customer walked away from checkout |
| `checkout_failed` | Checkout could not be started (Stripe refused, bad return URLs) |
| `checkout_not_configured` | The app has not connected Stripe for web payments |
| `already_subscribed` | The customer already has an active web subscription; change or cancel it instead |
| `checkout_unavailable` | Older Cloud servers without hosted checkout (kept for compatibility) |
| `rate_limited` | Too many requests |
| `server_error` | Cloud faulted |
| `unknown` | Anything else |

`err.paymentRequired` and `err.userCancelled` are shortcuts for the two you will
branch on most. A network failure is a typed error too — a raw `TypeError` from
`fetch` would be indistinguishable from a bug in your own code.

---

## React

```tsx
import { createContext, useContext, useEffect, useState } from 'react'
import { CashierCloud, type CustomerInfo } from '@codesyncr/cashier-cloud'

const cashier = new CashierCloud({ apiKey: import.meta.env.VITE_CASHIER_KEY })
const Ctx = createContext<CustomerInfo | null>(null)

export function CashierProvider({ userId, children }: { userId?: string; children: React.ReactNode }) {
  const [info, setInfo] = useState<CustomerInfo | null>(null)

  useEffect(() => {
    const off = cashier.onCustomerInfoUpdate(setInfo)
    // logIn merges anything bought before sign-up; plain read otherwise.
    ;(userId ? cashier.logIn(userId) : cashier.customerInfo()).catch(console.error)
    return off
  }, [userId])

  return <Ctx.Provider value={info}>{children}</Ctx.Provider>
}

export function useEntitlement(id: string) {
  return useContext(Ctx)?.entitlements[id]?.active ?? false
}
```

---

## Types

Everything is exported: `CustomerInfo`, `EntitlementInfo`, `Product`, `Package`,
`Offering`, `ResolvedPackage`, `OfferingsResponse`, `Subscription`,
`SubscriptionStatus`, `PeriodType`, `EntitlementSource`, `Entitlement`.

### Webhooks on your backend

Cashier posts every subscriber event to your webhook URL, signed with your
app's webhook secret. `verifyCashierWebhook` checks the `Cashier-Signature`
header over the **raw** body and returns the typed payload (Node 18+, Deno,
Bun, edge runtimes):

```typescript
import { verifyCashierWebhook } from '@codesyncr/cashier-cloud'

// Express: app.post('/cashier', express.raw({ type: 'application/json' }), handler)
export async function handler(req, res) {
  const hook = await verifyCashierWebhook(
    process.env.CASHIER_WEBHOOK_SECRET!,
    req.header('cashier-signature'),
    req.body,                         // the raw bytes, not parsed JSON
  )
  if (!hook) return res.status(400).end()

  const e = hook.event                // snake_case, exactly as sent
  switch (e.type) {
    case 'initial_purchase':
    case 'renewal':        await grant(e.app_user_id, e.entitlement_ids); break
    case 'billing_issue':  await emailAboutPayment(e.app_user_id); break
    case 'expiration':     await revoke(e.app_user_id, e.entitlement_ids); break
    case 'paywall_response': await saveAnswer(e.app_user_id, e.question, e.answer); break
  }
  res.status(200).end()               // anything else is retried: 1m, 5m, 30m, 2h, 6h, 12h
}
```

Type a handler that doesn't verify with `CashierWebhook` / `CashierWebhookEvent`.
`SubscriberEvent` is the SDK's camelCase shape and is not what webhooks carry.

---

## Not in this package

- **Native in-app purchases** — the iOS and Android SDKs, which wrap StoreKit 2
  and Play Billing.
- **Receipt verification and metering** — Cloud's, and they run on Cloud.
- **Razorpay, PayU, PhonePe, Cashfree, Paytm, PayPal** — the multi-gateway layer
  is `plugins/cashier` in the Nimbus framework: free, self-hosted, Go, and
  unrelated to Cloud.

---

## Development

```bash
npm install && npm run build && npm test
```

## Status

Private until Cloud's endpoints are live. The API surface below is settled.
