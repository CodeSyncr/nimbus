package iap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

/*
The Nimbus Cloud meter.

This is the enforced gate. It reports each transaction to Nimbus Cloud, which
holds the authoritative tracked volume for the account and rules on access. The
plugin never decides billing itself — it asks, and obeys — so a developer
cannot uncap the free tier by editing the client.

Failure policy, restated because it is the whole ethics of a payment gate:

  - A 402/403 (account not entitled: no plan, unpaid, suspended) fails CLOSED.
    That is the genuine "you have not paid for this feature" case.
  - Any other failure — network, timeout, 5xx — fails OPEN. A Nimbus Cloud
    outage must never stop a real customer completing a purchase. The
    transaction is marked for reconciliation so Cloud can bill it once it
    recovers, and the entitlement is granted meanwhile.
*/

// CloudMeter reports transactions to Nimbus Cloud and enforces its decisions.
type CloudMeter struct {
	apiKey   string
	endpoint string
	http     *http.Client
	// failOpen grants access when Cloud is unreachable. On by default; a
	// deployment that would rather block than risk an unbilled transaction can
	// turn it off.
	failOpen bool
}

// CloudMeterConfig configures the Cloud meter.
type CloudMeterConfig struct {
	// APIKey authenticates the developer's Nimbus Cloud account. Required —
	// without it there is no account to meter, so the gate fails closed.
	APIKey string
	// Endpoint overrides the metering URL. Defaults to Nimbus Cloud.
	Endpoint string
	// HTTPClient overrides the client (tests point it at a mock server).
	HTTPClient *http.Client
	// FailClosed makes a Cloud outage deny access instead of granting it. Off
	// by default; see the failure policy above before turning it on.
	FailClosed bool
}

// NewCloudMeter builds a meter backed by Nimbus Cloud.
func NewCloudMeter(cfg CloudMeterConfig) (*CloudMeter, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("cashier/iap: a Nimbus Cloud API key is required for in-app-purchase verification")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://api.nimbuscloud.io/v1/iap"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &CloudMeter{
		apiKey:   cfg.APIKey,
		endpoint: strings.TrimRight(endpoint, "/"),
		http:     client,
		failOpen: !cfg.FailClosed,
	}, nil
}

type authorizeRequest struct {
	Platform      string `json:"platform"`
	TransactionID string `json:"transaction_id"`
	ProductID     string `json:"product_id"`
	Subject       string `json:"subject"`
	PriceMicros   int64  `json:"price_micros"`
	Currency      string `json:"currency"`
	Environment   string `json:"environment"`
}

type authorizeResponse struct {
	Allowed             bool   `json:"allowed"`
	Reason              string `json:"reason"`
	TrackedVolumeMicros int64  `json:"tracked_volume_micros"`
	FeeMicros           int64  `json:"fee_micros"`
	OverFreeTier        bool   `json:"over_free_tier"`
}

// Authorize reports a transaction and returns Cloud's decision.
func (m *CloudMeter) Authorize(ctx context.Context, t MeteredTransaction) (Decision, error) {
	// Sandbox and zero-price events are free by definition; do not spend a
	// round-trip or risk an outage blocking them.
	if !t.Billable() {
		return Decision{Allowed: true, Reason: "not billable"}, nil
	}

	body, _ := json.Marshal(authorizeRequest{
		Platform:      string(t.Platform),
		TransactionID: t.TransactionID,
		ProductID:     t.ProductID,
		Subject:       t.Subject,
		PriceMicros:   t.PriceMicros,
		Currency:      t.Currency,
		Environment:   t.Environment,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint+"/authorize", bytes.NewReader(body))
	if err != nil {
		return m.onOutage(t, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := m.http.Do(req)
	if err != nil {
		return m.onOutage(t, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// A definitive "not entitled" is the unpaid-account case: fail closed.
	if resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden {
		reason := strings.TrimSpace(string(raw))
		if reason == "" {
			reason = "account is not entitled to in-app-purchase verification"
		}
		return Decision{Allowed: false, Reason: reason}, nil
	}
	// Any other non-2xx is treated as an outage: fail open, reconcile later.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return m.onOutage(t, fmt.Errorf("nimbus cloud returned %d", resp.StatusCode))
	}

	var out authorizeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return m.onOutage(t, err)
	}
	return Decision{
		Allowed:             out.Allowed,
		Reason:              out.Reason,
		TrackedVolumeMicros: out.TrackedVolumeMicros,
		FeeMicros:           out.FeeMicros,
		OverFreeTier:        out.OverFreeTier,
	}, nil
}

// onOutage applies the failure policy for a transient error.
func (m *CloudMeter) onOutage(t MeteredTransaction, cause error) (Decision, error) {
	if m.failOpen {
		// Grant access, flag for reconciliation. The purchase completes; Cloud
		// bills the transaction once it can.
		return Decision{Allowed: true, Reason: "granted during a metering outage", Reconcile: true}, nil
	}
	return Decision{Allowed: false, Reason: fmt.Sprintf("metering unavailable: %v", cause)}, nil
}
