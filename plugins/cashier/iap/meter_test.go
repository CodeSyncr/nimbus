package iap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

func tx(priceMicros int64, env string) MeteredTransaction {
	return MeteredTransaction{
		Platform: contracts.PlatformApple, TransactionID: "t", ProductID: "p",
		PriceMicros: priceMicros, Currency: "USD", Environment: env,
	}
}

// The whole billing model in one test: nothing is charged until $2000 of
// tracked volume, then 1% of the overage — and a transaction straddling the
// boundary is billed only on the part above it.
func TestFreeTierThenOnePercent(t *testing.T) {
	cases := []struct {
		name     string
		prior    int64
		amount   int64
		wantFee  int64
		wantOver bool
	}{
		{"well under the free tier", 0, dollars(100), 0, false},
		{"exactly at the free tier", dollars(1900), dollars(100), 0, false},
		{"first dollar over", dollars(2000), dollars(100), dollars(100) / 100, true},
		{"straddling the boundary", dollars(1950), dollars(100), dollars(50) / 100, true},
		{"fully over", dollars(5000), dollars(200), dollars(200) / 100, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fee, over := feeFor(c.prior, c.amount)
			if fee != c.wantFee {
				t.Errorf("fee = %d micros, want %d", fee, c.wantFee)
			}
			if over != c.wantOver {
				t.Errorf("over = %v, want %v", over, c.wantOver)
			}
		})
	}
}

// A local meter accumulates volume and applies the same rule end to end.
func TestLocalMeterAccumulates(t *testing.T) {
	m := NewLocalMeter()
	ctx := context.Background()

	// $1500, still free.
	d, _ := m.Authorize(ctx, tx(dollars(1500), "production"))
	if d.FeeMicros != 0 || d.OverFreeTier {
		t.Errorf("under the tier should be free: %+v", d)
	}
	// Another $1000 → $2500 total, $500 over → 1% = $5 fee on the overage.
	d, _ = m.Authorize(ctx, tx(dollars(1000), "production"))
	if !d.OverFreeTier {
		t.Error("crossing the tier should be flagged")
	}
	if d.FeeMicros != dollars(5) {
		t.Errorf("fee = %d, want $5 in micros (%d)", d.FeeMicros, dollars(5))
	}
	if m.TrackedVolume() != dollars(2500) {
		t.Errorf("tracked volume = %d, want $2500", m.TrackedVolume())
	}
}

// Sandbox transactions are never metered — a tester must not consume the free
// tier or cost money.
func TestSandboxIsNeverMetered(t *testing.T) {
	m := NewLocalMeter()
	for i := 0; i < 100; i++ {
		_, _ = m.Authorize(context.Background(), tx(dollars(1000), "sandbox"))
	}
	if m.TrackedVolume() != 0 {
		t.Errorf("sandbox volume was tracked: %d", m.TrackedVolume())
	}
}

// A zero-price event (a store that did not report a price) is not billable.
func TestZeroPriceIsNotBillable(t *testing.T) {
	if tx(0, "production").Billable() {
		t.Error("a zero-price transaction must not be billable")
	}
}

func dollars(n int64) int64 { return n * 1_000_000 }

// The Cloud meter must fail CLOSED when Cloud says the account is not entitled.
func TestCloudMeterFailsClosedOnDenial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "account suspended for non-payment", http.StatusPaymentRequired)
	}))
	defer srv.Close()

	m, _ := NewCloudMeter(CloudMeterConfig{APIKey: "k", Endpoint: srv.URL, HTTPClient: srv.Client()})
	d, err := m.Authorize(context.Background(), tx(dollars(10), "production"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if d.Allowed {
		t.Error("an unpaid account must be denied access")
	}
}

// It must fail OPEN on a transient outage — a Cloud problem must never break a
// real purchase — and flag the transaction for reconciliation.
func TestCloudMeterFailsOpenOnOutage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, _ := NewCloudMeter(CloudMeterConfig{APIKey: "k", Endpoint: srv.URL, HTTPClient: srv.Client()})
	d, err := m.Authorize(context.Background(), tx(dollars(10), "production"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !d.Allowed {
		t.Error("a Cloud outage must not block a real purchase")
	}
	if !d.Reconcile {
		t.Error("a granted-during-outage transaction must be flagged for reconciliation")
	}
}

// With FailClosed set, an outage denies instead.
func TestCloudMeterFailClosedOption(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	m, _ := NewCloudMeter(CloudMeterConfig{APIKey: "k", Endpoint: srv.URL, HTTPClient: srv.Client(), FailClosed: true})
	d, _ := m.Authorize(context.Background(), tx(dollars(10), "production"))
	if d.Allowed {
		t.Error("FailClosed should deny on an outage")
	}
}

// A happy-path decision is carried through from Cloud.
func TestCloudMeterCarriesDecision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("API key not sent: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(authorizeResponse{
			Allowed: true, TrackedVolumeMicros: dollars(2500), FeeMicros: dollars(5), OverFreeTier: true,
		})
	}))
	defer srv.Close()

	m, _ := NewCloudMeter(CloudMeterConfig{APIKey: "k", Endpoint: srv.URL, HTTPClient: srv.Client()})
	d, err := m.Authorize(context.Background(), tx(dollars(1000), "production"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !d.Allowed || !d.OverFreeTier || d.FeeMicros != dollars(5) {
		t.Errorf("decision not carried through: %+v", d)
	}
}

// A meter needs an API key: without an account there is nothing to meter.
func TestCloudMeterRequiresKey(t *testing.T) {
	if _, err := NewCloudMeter(CloudMeterConfig{}); err == nil {
		t.Fatal("built a meter with no API key")
	}
}
