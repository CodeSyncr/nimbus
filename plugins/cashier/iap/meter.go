package iap

import (
	"context"
	"fmt"
	"sync"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

/*
Metering.

Apple and Google verification is a paid Nimbus Cloud feature, priced like
RevenueCat: the first $2000 of tracked transaction volume is free, and beyond
that each transaction costs 1% of its value. The gate is what makes it paid —
a verified entitlement is only returned once the meter authorises it.

Two rules keep the gate from ever hurting the developer's own customers:

  - Sandbox transactions are never tracked and never billed. A tester must not
    cost money, and a sandbox receipt must not consume the free tier.
  - A transient failure to reach Nimbus Cloud fails OPEN, not closed. A metering
    outage must never block a real purchase; the transaction is reported for
    reconciliation and the customer gets their entitlement. Only a definitive
    "this account may not use IAP" (missing, invalid or suspended key) fails
    closed, because that is the actual unpaid-account case.
*/

const (
	// FreeTierMicros is the tracked transaction volume that is free, in micros
	// (2000 * 1_000_000). Past this, transactions are billable.
	FreeTierMicros int64 = 2000 * 1_000_000
	// FeeRatePerMille is the fee beyond the free tier, in parts per thousand:
	// 10‰ = 1%.
	FeeRatePerMille int64 = 10
)

// MeteredTransaction is one verified purchase reported for metering.
type MeteredTransaction struct {
	Platform      contracts.IAPPlatform
	TransactionID string
	ProductID     string
	Subject       string
	PriceMicros   int64
	Currency      string
	Environment   string // "sandbox" transactions are never metered
}

// Billable reports whether a transaction counts toward volume and fees at all.
// A sandbox purchase or a zero-price event never does.
func (t MeteredTransaction) Billable() bool {
	return t.Environment != "sandbox" && t.PriceMicros > 0
}

// Decision is the meter's ruling on a transaction.
type Decision struct {
	// Allowed is whether the entitlement may be returned to the app.
	Allowed bool
	// Reason explains a denial, for logs and errors.
	Reason string
	// TrackedVolumeMicros is the account's cumulative tracked volume after this
	// transaction, as the meter understands it.
	TrackedVolumeMicros int64
	// FeeMicros is what this transaction cost (0 within the free tier).
	FeeMicros int64
	// OverFreeTier is whether the account has exhausted the free allowance.
	OverFreeTier bool
	// Reconcile is set when the decision was made locally after a Cloud outage
	// and must be re-reported later. The entitlement was still granted.
	Reconcile bool
}

// Meter authorises and records IAP transactions for billing.
type Meter interface {
	// Authorize records a transaction and rules on whether its entitlement may
	// be granted. It must fail open on transient errors (see the package note).
	Authorize(ctx context.Context, t MeteredTransaction) (Decision, error)
}

// feeFor returns the fee for adding `amount` of billable volume to an account
// already at `priorVolume`, in micros. Only the portion above the free tier is
// charged, so a transaction straddling the boundary is billed on its overage
// alone.
func feeFor(priorVolume, amount int64) (fee int64, overFree bool) {
	newVolume := priorVolume + amount
	if newVolume <= FreeTierMicros {
		return 0, false
	}
	// Billable portion is whatever of this transaction sits above the free tier.
	billable := amount
	if priorVolume < FreeTierMicros {
		billable = newVolume - FreeTierMicros
	}
	return billable * FeeRatePerMille / 1000, true
}

// LocalMeter is a process-local Meter: it applies the exact tier and fee rules
// without a network call. It is the default for development and the reference
// the Cloud meter is tested against, and it always allows — enforcement of an
// unpaid account is Nimbus Cloud's job, not a local cache's.
type LocalMeter struct {
	mu       sync.Mutex
	byTenant map[string]int64 // subject-agnostic; keyed by tenant, default ""
	tenant   string
}

// NewLocalMeter builds an in-memory meter for one tenant.
func NewLocalMeter() *LocalMeter {
	return &LocalMeter{byTenant: map[string]int64{}}
}

// Authorize records the transaction locally and computes its fee.
func (m *LocalMeter) Authorize(_ context.Context, t MeteredTransaction) (Decision, error) {
	if !t.Billable() {
		// Sandbox or zero-price: allowed, tracked at zero, never billed.
		return Decision{Allowed: true, Reason: "not billable"}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prior := m.byTenant[m.tenant]
	fee, over := feeFor(prior, t.PriceMicros)
	m.byTenant[m.tenant] = prior + t.PriceMicros
	return Decision{
		Allowed:             true,
		TrackedVolumeMicros: prior + t.PriceMicros,
		FeeMicros:           fee,
		OverFreeTier:        over,
	}, nil
}

// TrackedVolume returns the tenant's accumulated volume, for tests and reports.
func (m *LocalMeter) TrackedVolume() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byTenant[m.tenant]
}

// meteredVerifier wraps an IAPVerifier so every returned entitlement passes
// through the meter. This is what ties verification to the paid gate: the
// wrapped verifier does the crypto, the meter rules on access.
type meteredVerifier struct {
	inner contracts.IAPVerifier
	meter Meter
}

func (v meteredVerifier) Platform() contracts.IAPPlatform { return v.inner.Platform() }

func (v meteredVerifier) VerifyReceipt(ctx context.Context, p contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	ent, err := v.inner.VerifyReceipt(ctx, p)
	if err != nil {
		return nil, err
	}
	// Only a live, granting entitlement is a billable event; a lapsed or
	// refunded receipt is verified but earns nothing and is not metered.
	if !ent.Active {
		return ent, nil
	}
	dec, err := v.meter.Authorize(ctx, MeteredTransaction{
		Platform:      ent.Platform,
		TransactionID: ent.TransactionID,
		ProductID:     ent.ProductID,
		Subject:       ent.Subject,
		PriceMicros:   ent.PriceMicros,
		Currency:      ent.Currency,
		Environment:   ent.Environment,
	})
	if err != nil {
		return nil, err
	}
	if !dec.Allowed {
		return nil, fmt.Errorf("cashier/iap: Nimbus Cloud denied IAP access: %s", dec.Reason)
	}
	return ent, nil
}

func (v meteredVerifier) ParseNotification(payload []byte) (*contracts.StoreNotification, error) {
	return v.inner.ParseNotification(payload)
}

// Metered wraps a verifier so its entitlements are gated by a meter. Registering
// the result with IAPManager is what turns a raw verifier into the paid feature.
func Metered(v contracts.IAPVerifier, m Meter) contracts.IAPVerifier {
	return meteredVerifier{inner: v, meter: m}
}
