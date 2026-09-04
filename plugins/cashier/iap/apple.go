// Package iap holds the Apple and Google in-app-purchase verifiers. They live
// outside the cashier package so their heavier crypto and HTTP dependencies do
// not weigh on callers who only use card gateways.
package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
	"github.com/golang-jwt/jwt/v5"
)

/*
Apple StoreKit 2 / App Store Server API verification.

StoreKit 2 hands the app a signed JWS transaction, and App Store Server
Notifications V2 arrive as a signed JWS too. The trust model is the x5c header:
each JWS carries its signing certificate chain, and a payload is trustworthy
only if that chain terminates at Apple's known root. Verifying the signature
without validating the chain to Apple's root would accept a token anyone could
mint — so the chain check is the load-bearing part here, not an optional extra.

No network call is needed to verify a StoreKit 2 JWS: everything required is in
the token and the pinned Apple root. That is the whole point of the V2 design.
*/

// AppleVerifier verifies StoreKit 2 JWS transactions and V2 notifications.
type AppleVerifier struct {
	// bundleID is the app the receipts must belong to; a transaction for a
	// different bundle is rejected even if Apple signed it.
	bundleID string
	// roots are the trusted Apple root certificates. Chains must terminate here.
	roots *x509.CertPool
	// allowSandbox permits sandbox-signed receipts. Off in production so a
	// tester's sandbox purchase cannot be replayed as a real entitlement.
	allowSandbox bool
}

// AppleConfig configures the Apple verifier.
type AppleConfig struct {
	BundleID     string
	AllowSandbox bool
	// RootCertsPEM is Apple's root certificate(s) in PEM. Required: without a
	// pinned root there is nothing to anchor the chain to. Apple publishes the
	// "Apple Root CA - G3" certificate for this.
	RootCertsPEM []byte
}

// NewApple builds the Apple verifier.
func NewApple(cfg AppleConfig) (*AppleVerifier, error) {
	if cfg.BundleID == "" {
		return nil, fmt.Errorf("cashier/iap/apple: BundleID is required")
	}
	if len(cfg.RootCertsPEM) == 0 {
		return nil, fmt.Errorf("cashier/iap/apple: RootCertsPEM is required to anchor the certificate chain")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cfg.RootCertsPEM) {
		return nil, fmt.Errorf("cashier/iap/apple: RootCertsPEM contained no usable certificate")
	}
	return &AppleVerifier{bundleID: cfg.BundleID, roots: pool, allowSandbox: cfg.AllowSandbox}, nil
}

func (a *AppleVerifier) Platform() contracts.IAPPlatform { return contracts.PlatformApple }

// appleTransaction is the JWS payload of a StoreKit 2 signed transaction.
type appleTransaction struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	Type                  string `json:"type"` // "Auto-Renewable Subscription" | "Consumable" | …
	PurchaseDate          int64  `json:"purchaseDate"`
	ExpiresDate           int64  `json:"expiresDate"`
	Environment           string `json:"environment"` // "Production" | "Sandbox"
	RevocationDate        int64  `json:"revocationDate"`
	// Price is in milliunits of Currency in StoreKit 2 (e.g. 4990 = 4.99).
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
}

// VerifyReceipt verifies a StoreKit 2 signed transaction and returns the
// entitlement it proves.
func (a *AppleVerifier) VerifyReceipt(ctx context.Context, p contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	if p.Token == "" {
		return nil, fmt.Errorf("cashier/iap/apple: the signed transaction (Token) is required")
	}
	var tx appleTransaction
	if err := a.verifyJWS(p.Token, &tx); err != nil {
		return nil, err
	}
	if tx.BundleID != a.bundleID {
		return nil, fmt.Errorf("cashier/iap/apple: transaction is for bundle %q, not %q", tx.BundleID, a.bundleID)
	}
	if p.ProductID != "" && tx.ProductID != p.ProductID {
		return nil, fmt.Errorf("cashier/iap/apple: transaction is for product %q, not the claimed %q", tx.ProductID, p.ProductID)
	}
	if !a.allowSandbox && !isProd(tx.Environment) {
		return nil, fmt.Errorf("cashier/iap/apple: refusing a %s receipt on a production verifier", tx.Environment)
	}

	ent := &contracts.IAPEntitlement{
		Platform:              contracts.PlatformApple,
		ProductID:             tx.ProductID,
		Subject:               p.Subject,
		TransactionID:         tx.TransactionID,
		OriginalTransactionID: tx.OriginalTransactionID,
		Subscription:          tx.ExpiresDate > 0,
		Environment:           envLabel(tx.Environment),
		PriceMicros:           tx.Price * 1000, // milliunits → micros
		Currency:              tx.Currency,
		Raw:                   map[string]any{"type": tx.Type},
	}
	if tx.ExpiresDate > 0 {
		exp := msToTime(tx.ExpiresDate)
		ent.ExpiresAt = &exp
		ent.Active = tx.RevocationDate == 0 && time.Now().Before(exp)
	} else {
		// A non-consumable or consumable purchase is owned once verified,
		// unless it was revoked (refunded).
		ent.Active = tx.RevocationDate == 0
	}
	return ent, nil
}

// appleNotification is the outer V2 notification payload.
type appleNotification struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	Data             struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
		BundleID              string `json:"bundleId"`
	} `json:"data"`
}

// ParseNotification verifies an App Store Server Notification V2 and reduces it
// to the canonical shape.
func (a *AppleVerifier) ParseNotification(payload []byte) (*contracts.StoreNotification, error) {
	var wrap struct {
		SignedPayload string `json:"signedPayload"`
	}
	if err := json.Unmarshal(payload, &wrap); err != nil || wrap.SignedPayload == "" {
		return nil, fmt.Errorf("cashier/iap/apple: notification has no signedPayload")
	}
	var note appleNotification
	if err := a.verifyJWS(wrap.SignedPayload, &note); err != nil {
		return nil, err
	}

	out := &contracts.StoreNotification{
		Platform: contracts.PlatformApple,
		Type:     appleNoteType(note.NotificationType, note.Subtype),
		Raw:      payload,
	}
	// The nested transaction, when present, carries the product and expiry.
	if note.Data.SignedTransactionInfo != "" {
		var tx appleTransaction
		if err := a.verifyJWS(note.Data.SignedTransactionInfo, &tx); err == nil {
			out.ProductID = tx.ProductID
			out.OriginalTransactionID = tx.OriginalTransactionID
			if tx.ExpiresDate > 0 {
				exp := msToTime(tx.ExpiresDate)
				out.ExpiresAt = &exp
			}
		}
	}
	return out, nil
}

// verifyJWS validates a StoreKit JWS: the ES256 signature against the leaf
// certificate's key, and the leaf's chain up to a pinned Apple root. Only then
// is the payload decoded into out.
func (a *AppleVerifier) verifyJWS(token string, out any) error {
	var leaf *x509.Certificate
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "ES256" {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		chain, err := x5cChain(t)
		if err != nil {
			return nil, err
		}
		leaf = chain[0]
		if err := verifyAppleChain(chain, a.roots); err != nil {
			return nil, err
		}
		pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("leaf certificate is not ECDSA")
		}
		return pub, nil
	})
	if err != nil {
		return fmt.Errorf("cashier/iap/apple: JWS verification failed: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("cashier/iap/apple: unexpected claims shape")
	}
	// Re-encode the claims to decode into the typed struct.
	b, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// x5cChain decodes the x5c header into a certificate chain, leaf first.
func x5cChain(t *jwt.Token) ([]*x509.Certificate, error) {
	raw, ok := t.Header["x5c"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("JWS has no x5c certificate chain")
	}
	chain := make([]*x509.Certificate, 0, len(raw))
	for _, entry := range raw {
		s, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("x5c entry is not a string")
		}
		der, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("x5c entry is not valid base64: %w", err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("x5c entry is not a valid certificate: %w", err)
		}
		chain = append(chain, cert)
	}
	return chain, nil
}

// verifyAppleChain checks that the leaf chains up to a pinned root through the
// supplied intermediates.
func verifyAppleChain(chain []*x509.Certificate, roots *x509.CertPool) error {
	inter := x509.NewCertPool()
	for _, c := range chain[1:] {
		inter.AddCert(c)
	}
	_, err := chain[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: inter,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return fmt.Errorf("certificate chain does not terminate at a trusted Apple root: %w", err)
	}
	return nil
}

func isProd(env string) bool { return env == "" || env == "Production" || env == "PROD" }

func envLabel(env string) string {
	if isProd(env) {
		return "production"
	}
	return "sandbox"
}

func msToTime(ms int64) time.Time { return time.Unix(0, ms*int64(time.Millisecond)) }

// appleNoteType maps Apple's V2 notification types onto the canonical set.
func appleNoteType(t, sub string) string {
	switch t {
	case "DID_RENEW", "SUBSCRIBED":
		return "renewed"
	case "DID_CHANGE_RENEWAL_STATUS":
		if sub == "AUTO_RENEW_DISABLED" {
			return "canceled"
		}
		return "renewed"
	case "EXPIRED":
		return "expired"
	case "REFUND":
		return "refunded"
	case "GRACE_PERIOD_EXPIRED":
		return "expired"
	case "DID_FAIL_TO_RENEW":
		return "grace_period"
	default:
		return t
	}
}
