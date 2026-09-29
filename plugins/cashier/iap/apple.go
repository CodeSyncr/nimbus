// Package iap holds the Apple and Google in-app-purchase verifiers. They live
// outside the cashier package so their heavier crypto and HTTP dependencies do
// not weigh on callers who only use card gateways.
package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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

// Apple marks the certificates it uses for App Store receipts with its own
// extensions. Apple Root CA - G3 also anchors the developer-relations
// intermediates that issue ordinary developer certificates, so a chain that
// merely reaches the root is not enough: without these checks any developer
// could sign a "transaction" with their own certificate and pass.
var (
	oidAppleReceiptSigner = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
	oidAppleIntermediate  = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
)

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
	// pinned root there is nothing to anchor the chain to. AppleRootCAG3PEM is
	// the one Apple uses.
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
	AppAccountToken       string `json:"appAccountToken"`
	// OfferType: 1 introductory, 2 promotional, 3 offer code, 4 win-back.
	OfferType int `json:"offerType"`
	// OfferDiscountType: "FREE_TRIAL" | "PAY_AS_YOU_GO" | "PAY_UP_FRONT".
	OfferDiscountType string `json:"offerDiscountType"`
	// Price is in milliunits of Currency in StoreKit 2 (e.g. 4990 = 4.99).
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
}

// appleRenewalInfo is the JWS payload of a subscription's signed renewal info.
type appleRenewalInfo struct {
	OriginalTransactionID  string `json:"originalTransactionId"`
	AutoRenewProductID     string `json:"autoRenewProductId"`
	AutoRenewStatus        int    `json:"autoRenewStatus"` // 1 on, 0 off
	IsInBillingRetryPeriod bool   `json:"isInBillingRetryPeriod"`
	GracePeriodExpiresDate int64  `json:"gracePeriodExpiresDate"`
}

// VerifyReceipt verifies a StoreKit 2 signed transaction (and, when the app
// sends it, the signed renewal info) and returns the entitlement it proves.
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

	var renewal *appleRenewalInfo
	if p.RenewalInfo != "" {
		var ri appleRenewalInfo
		if err := a.verifyJWS(p.RenewalInfo, &ri); err != nil {
			return nil, err
		}
		// Renewal info for a different subscription would let a client pair
		// one purchase with another's grace period.
		if ri.OriginalTransactionID != tx.OriginalTransactionID {
			return nil, fmt.Errorf("cashier/iap/apple: renewal info belongs to another subscription")
		}
		renewal = &ri
	}

	ent := appleEntitlement(tx, renewal)
	ent.Subject = p.Subject
	return ent, nil
}

// appleEntitlement reduces a verified transaction (and renewal info, when
// known) to the canonical entitlement.
func appleEntitlement(tx appleTransaction, renewal *appleRenewalInfo) *contracts.IAPEntitlement {
	ent := &contracts.IAPEntitlement{
		Platform:              contracts.PlatformApple,
		ProductID:             tx.ProductID,
		TransactionID:         tx.TransactionID,
		OriginalTransactionID: tx.OriginalTransactionID,
		Subscription:          tx.ExpiresDate > 0,
		Environment:           envLabel(tx.Environment),
		PriceMicros:           tx.Price * 1000, // milliunits → micros
		Currency:              tx.Currency,
		Revoked:               tx.RevocationDate != 0,
		AppAccountToken:       tx.AppAccountToken,
		PeriodType:            applePeriodType(tx),
		Raw:                   map[string]any{"type": tx.Type},
	}
	if tx.PurchaseDate > 0 {
		at := msToTime(tx.PurchaseDate)
		ent.PurchasedAt = &at
	}
	if !ent.Subscription {
		// A non-consumable or consumable purchase is owned once verified,
		// unless it was revoked (refunded).
		ent.Active = !ent.Revoked
		return ent
	}

	exp := msToTime(tx.ExpiresDate)
	ent.ExpiresAt = &exp
	// Without renewal info, assume the subscription renews: that is the store
	// default, and a cancellation arrives as a notification.
	ent.AutoRenewing = true
	accessUntil := exp
	if renewal != nil {
		ent.AutoRenewing = renewal.AutoRenewStatus == 1
		ent.BillingIssue = renewal.IsInBillingRetryPeriod
		if renewal.GracePeriodExpiresDate > 0 {
			grace := msToTime(renewal.GracePeriodExpiresDate)
			ent.GraceExpiresAt = &grace
			if grace.After(accessUntil) {
				accessUntil = grace
			}
		}
	}
	ent.Active = !ent.Revoked && time.Now().Before(accessUntil)
	return ent
}

func applePeriodType(tx appleTransaction) string {
	if tx.ExpiresDate == 0 {
		return ""
	}
	if tx.OfferType != 1 {
		return "normal"
	}
	if tx.OfferDiscountType == "FREE_TRIAL" || (tx.OfferDiscountType == "" && tx.Price == 0) {
		return "trial"
	}
	return "intro"
}

// appleNotification is the outer V2 notification payload.
type appleNotification struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	NotificationUUID string `json:"notificationUUID"`
	Data             struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
		SignedRenewalInfo     string `json:"signedRenewalInfo"`
		BundleID              string `json:"bundleId"`
		Environment           string `json:"environment"`
	} `json:"data"`
}

// ParseNotification verifies an App Store Server Notification V2 and reduces it
// to the canonical shape, including the purchase state it vouches for.
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
	if note.Data.BundleID != "" && note.Data.BundleID != a.bundleID {
		return nil, fmt.Errorf("cashier/iap/apple: notification is for bundle %q, not %q", note.Data.BundleID, a.bundleID)
	}

	out := &contracts.StoreNotification{
		Platform:    contracts.PlatformApple,
		Type:        appleNoteType(note.NotificationType, note.Subtype),
		Subtype:     strings.Trim(note.NotificationType+"/"+note.Subtype, "/"),
		ID:          note.NotificationUUID,
		Environment: envLabel(note.Data.Environment),
		Raw:         payload,
	}
	if note.Data.SignedTransactionInfo == "" {
		return out, nil // TEST, and summary notifications, carry no transaction
	}
	var tx appleTransaction
	if err := a.verifyJWS(note.Data.SignedTransactionInfo, &tx); err != nil {
		return nil, err
	}
	var renewal *appleRenewalInfo
	if note.Data.SignedRenewalInfo != "" {
		var ri appleRenewalInfo
		if err := a.verifyJWS(note.Data.SignedRenewalInfo, &ri); err != nil {
			return nil, err
		}
		renewal = &ri
	}
	ent := appleEntitlement(tx, renewal)
	out.Entitlement = ent
	out.ProductID = tx.ProductID
	out.OriginalTransactionID = tx.OriginalTransactionID
	out.TransactionID = tx.TransactionID
	out.ExpiresAt = ent.ExpiresAt
	return out, nil
}

// verifyJWS validates a StoreKit JWS: the ES256 signature against the leaf
// certificate's key, and the leaf's chain up to a pinned Apple root. Only then
// is the payload decoded into out.
func (a *AppleVerifier) verifyJWS(token string, out any) error {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "ES256" {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		chain, err := x5cChain(t)
		if err != nil {
			return nil, err
		}
		if err := verifyAppleChain(chain, a.roots); err != nil {
			return nil, err
		}
		pub, ok := chain[0].PublicKey.(*ecdsa.PublicKey)
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

// verifyAppleChain checks that the leaf chains up to a pinned root through an
// Apple receipt intermediate, and that the leaf is a receipt-signing
// certificate.
func verifyAppleChain(chain []*x509.Certificate, roots *x509.CertPool) error {
	inter := x509.NewCertPool()
	for _, c := range chain[1:] {
		inter.AddCert(c)
	}
	verified, err := chain[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: inter,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return fmt.Errorf("certificate chain does not terminate at a trusted Apple root: %w", err)
	}
	if !hasExtension(chain[0], oidAppleReceiptSigner) {
		return fmt.Errorf("leaf certificate is not an App Store receipt-signing certificate")
	}
	for _, path := range verified {
		// leaf → Apple intermediate → root.
		if len(path) == 3 && hasExtension(path[1], oidAppleIntermediate) {
			return nil
		}
	}
	return fmt.Errorf("certificate chain does not pass through an Apple receipt intermediate")
}

func hasExtension(cert *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oid) {
			return true
		}
	}
	return false
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
	case "SUBSCRIBED", "ONE_TIME_CHARGE":
		return "purchased"
	case "DID_RENEW":
		if sub == "BILLING_RECOVERY" {
			return "recovered"
		}
		return "renewed"
	case "DID_CHANGE_RENEWAL_STATUS":
		if sub == "AUTO_RENEW_DISABLED" {
			return "canceled"
		}
		return "uncanceled"
	case "DID_CHANGE_RENEWAL_PREF":
		return "product_change"
	case "DID_FAIL_TO_RENEW":
		if sub == "GRACE_PERIOD" {
			return "grace_period"
		}
		return "billing_issue"
	case "EXPIRED", "GRACE_PERIOD_EXPIRED":
		return "expired"
	case "REFUND", "REVOKE":
		return "refunded"
	case "REFUND_REVERSED":
		return "refund_reversed"
	case "RENEWAL_EXTENDED":
		return "extended"
	case "TEST":
		return "test"
	default:
		return strings.ToLower(t)
	}
}
