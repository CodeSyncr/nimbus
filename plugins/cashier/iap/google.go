package iap

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
	"github.com/golang-jwt/jwt/v5"
)

/*
Google Play Billing verification.

Unlike Apple, Google's purchase token is opaque: it proves nothing on its own,
and the entitlement lives behind the Android Publisher API. Verification is
therefore a network call, authenticated as a service account. The flow is the
OAuth2 JWT-bearer grant — sign a JWT with the service-account key, exchange it
for an access token, then query the purchase — and the access token is cached
until it nears expiry so a burst of verifications is one token exchange, not
one per purchase.

The rule that matters: the purchase token from the client is only a lookup key.
What Google's API returns is the entitlement; the client's claim about what it
bought is never trusted.
*/

// GoogleVerifier verifies Google Play purchases via the Android Publisher API.
type GoogleVerifier struct {
	packageName string
	sa          googleServiceAccount
	http        *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// GoogleConfig configures the Google verifier.
type GoogleConfig struct {
	// PackageName is the app's package; a purchase is looked up under it.
	PackageName string
	// ServiceAccountJSON is the Google Cloud service-account key with access to
	// the Play Developer API, as downloaded from the console.
	ServiceAccountJSON []byte
}

type googleServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// NewGoogle builds the Google verifier.
func NewGoogle(cfg GoogleConfig) (*GoogleVerifier, error) {
	if cfg.PackageName == "" {
		return nil, fmt.Errorf("cashier/iap/google: PackageName is required")
	}
	var sa googleServiceAccount
	if err := json.Unmarshal(cfg.ServiceAccountJSON, &sa); err != nil {
		return nil, fmt.Errorf("cashier/iap/google: ServiceAccountJSON is not valid: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, fmt.Errorf("cashier/iap/google: ServiceAccountJSON is missing client_email or private_key")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &GoogleVerifier{
		packageName: cfg.PackageName,
		sa:          sa,
		http:        &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (g *GoogleVerifier) Platform() contracts.IAPPlatform { return contracts.PlatformGoogle }

// googleSubPurchase is the subscriptionsv2 purchase resource, trimmed to what
// the entitlement needs.
type googleSubPurchase struct {
	SubscriptionState string `json:"subscriptionState"`
	LineItems         []struct {
		ProductID        string `json:"productId"`
		ExpiryTime       string `json:"expiryTime"`
		AutoRenewingPlan *struct {
			RecurringPrice *struct {
				CurrencyCode string `json:"currencyCode"`
				Units        string `json:"units"`
				Nanos        int64  `json:"nanos"`
			} `json:"recurringPrice"`
		} `json:"autoRenewingPlan"`
	} `json:"lineItems"`
	LatestOrderID string    `json:"latestOrderId"`
	TestPurchase  *struct{} `json:"testPurchase"`
}

// googleProductPurchase is the one-time products resource.
type googleProductPurchase struct {
	PurchaseState int    `json:"purchaseState"` // 0 = purchased, 1 = canceled, 2 = pending
	OrderID       string `json:"orderId"`
	ProductID     string `json:"productId"`
}

// VerifyReceipt looks a purchase up against the Android Publisher API.
func (g *GoogleVerifier) VerifyReceipt(ctx context.Context, p contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	if p.Token == "" {
		return nil, fmt.Errorf("cashier/iap/google: the purchase token (Token) is required")
	}
	if p.Subscription {
		return g.verifySubscription(ctx, p)
	}
	return g.verifyProduct(ctx, p)
}

func (g *GoogleVerifier) verifySubscription(ctx context.Context, p contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	path := fmt.Sprintf("/androidpublisher/v3/applications/%s/purchases/subscriptionsv2/tokens/%s",
		url.PathEscape(g.packageName), url.PathEscape(p.Token))
	var out googleSubPurchase
	if err := g.get(ctx, path, &out); err != nil {
		return nil, err
	}

	ent := &contracts.IAPEntitlement{
		Platform:              contracts.PlatformGoogle,
		Subject:               p.Subject,
		OriginalTransactionID: out.LatestOrderID,
		TransactionID:         out.LatestOrderID,
		Subscription:          true,
		Environment:           googleEnv(out.TestPurchase != nil),
		Raw:                   map[string]any{"subscriptionState": out.SubscriptionState},
	}
	if len(out.LineItems) > 0 {
		li := out.LineItems[0]
		ent.ProductID = li.ProductID
		if exp, err := time.Parse(time.RFC3339, li.ExpiryTime); err == nil {
			ent.ExpiresAt = &exp
		}
		if li.AutoRenewingPlan != nil && li.AutoRenewingPlan.RecurringPrice != nil {
			rp := li.AutoRenewingPlan.RecurringPrice
			// Google prices are units + nanos of the currency; micros is
			// units*1e6 + nanos/1000.
			units, _ := strconv.ParseInt(rp.Units, 10, 64)
			ent.PriceMicros = units*1_000_000 + rp.Nanos/1000
			ent.Currency = rp.CurrencyCode
		}
	}
	ent.Active = out.SubscriptionState == "SUBSCRIPTION_STATE_ACTIVE" ||
		out.SubscriptionState == "SUBSCRIPTION_STATE_IN_GRACE_PERIOD"
	ent.AutoRenewing = ent.Active
	if p.ProductID != "" && ent.ProductID != "" && ent.ProductID != p.ProductID {
		return nil, fmt.Errorf("cashier/iap/google: purchase is for product %q, not the claimed %q", ent.ProductID, p.ProductID)
	}
	return ent, nil
}

func (g *GoogleVerifier) verifyProduct(ctx context.Context, p contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	if p.ProductID == "" {
		return nil, fmt.Errorf("cashier/iap/google: ProductID is required to verify a one-time product")
	}
	path := fmt.Sprintf("/androidpublisher/v3/applications/%s/purchases/products/%s/tokens/%s",
		url.PathEscape(g.packageName), url.PathEscape(p.ProductID), url.PathEscape(p.Token))
	var out googleProductPurchase
	if err := g.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &contracts.IAPEntitlement{
		Platform:              contracts.PlatformGoogle,
		Subject:               p.Subject,
		ProductID:             p.ProductID,
		TransactionID:         out.OrderID,
		OriginalTransactionID: out.OrderID,
		Subscription:          false,
		Active:                out.PurchaseState == 0,
		Environment:           "production",
		Raw:                   map[string]any{"purchaseState": out.PurchaseState},
	}, nil
}

// googleRTDN is a Real-time Developer Notification, delivered base64-wrapped in
// a Pub/Sub push message.
type googleRTDN struct {
	Message struct {
		Data string `json:"data"`
	} `json:"message"`
}

type googleDeveloperNotification struct {
	PackageName              string `json:"packageName"`
	SubscriptionNotification *struct {
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		SubscriptionID   string `json:"subscriptionId"`
	} `json:"subscriptionNotification"`
}

// ParseNotification decodes a Real-time Developer Notification.
//
// Google's RTDNs are not signed the way Apple's are — authenticity comes from
// the Pub/Sub push being authenticated at the transport, not from a signature
// in the body — so this decodes and canonicalises rather than verifying a
// signature. The entitlement itself must still be confirmed by calling
// VerifyReceipt with the token inside.
func (g *GoogleVerifier) ParseNotification(payload []byte) (*contracts.StoreNotification, error) {
	var env googleRTDN
	if err := json.Unmarshal(payload, &env); err != nil || env.Message.Data == "" {
		return nil, fmt.Errorf("cashier/iap/google: notification has no message data")
	}
	decoded, err := base64.StdEncoding.DecodeString(env.Message.Data)
	if err != nil {
		return nil, fmt.Errorf("cashier/iap/google: notification data is not base64: %w", err)
	}
	var note googleDeveloperNotification
	if err := json.Unmarshal(decoded, &note); err != nil {
		return nil, fmt.Errorf("cashier/iap/google: notification body is not valid: %w", err)
	}
	out := &contracts.StoreNotification{Platform: contracts.PlatformGoogle, Raw: payload}
	if note.SubscriptionNotification != nil {
		out.Type = googleNoteType(note.SubscriptionNotification.NotificationType)
		out.ProductID = note.SubscriptionNotification.SubscriptionID
		out.OriginalTransactionID = note.SubscriptionNotification.PurchaseToken
	}
	return out, nil
}

// get performs an authenticated GET against the Android Publisher API.
func (g *GoogleVerifier) get(ctx context.Context, path string, out any) error {
	token, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://androidpublisher.googleapis.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cashier/iap/google: API error %d: %s", resp.StatusCode, string(raw))
	}
	return json.Unmarshal(raw, out)
}

// accessToken returns a cached OAuth2 token, minting a new one via the
// JWT-bearer grant when the cache is empty or nearly expired.
func (g *GoogleVerifier) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.token != "" && time.Now().Before(g.tokenExp.Add(-1*time.Minute)) {
		return g.token, nil
	}

	assertion, err := g.signedAssertion()
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cashier/iap/google: token exchange failed %d: %s", resp.StatusCode, string(raw))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return "", err
	}
	g.token = tok.AccessToken
	g.tokenExp = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return g.token, nil
}

// signedAssertion builds and signs the JWT-bearer assertion for the token
// exchange, scoped to the Android Publisher API.
func (g *GoogleVerifier) signedAssertion() (string, error) {
	key, err := parseRSAKey(g.sa.PrivateKey)
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   g.sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/androidpublisher",
		"aud":   g.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
}

// parseRSAKey decodes a PEM PKCS#8 or PKCS#1 RSA private key.
func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("cashier/iap/google: service-account private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cashier/iap/google: unsupported private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("cashier/iap/google: service-account key is not RSA")
	}
	return key, nil
}

func googleEnv(test bool) string {
	if test {
		return "sandbox"
	}
	return "production"
}

// googleNoteType maps Google's numeric notification types onto the canonical set.
func googleNoteType(t int) string {
	switch t {
	case 2: // RENEWED
		return "renewed"
	case 3: // CANCELED
		return "canceled"
	case 13: // EXPIRED
		return "expired"
	case 12: // REVOKED
		return "refunded"
	case 6: // IN_GRACE_PERIOD
		return "grace_period"
	default:
		return fmt.Sprintf("google_type_%d", t)
	}
}
