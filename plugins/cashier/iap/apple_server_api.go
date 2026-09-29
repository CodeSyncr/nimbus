package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
	"github.com/golang-jwt/jwt/v5"
)

/*
App Store Server API.

A StoreKit 2 JWS proves what the app held when it sent it; server
notifications keep that current. Both can be missed — an app never reopened, a
notification Apple gave up retrying — so a server that expires a subscription
on its own clock should first ask Apple. This client does that: it reads the
subscription's latest signed transaction and renewal info and reduces them
through the verifier, so the answer is trusted exactly as a notification is.

It needs an In-App Purchase key from App Store Connect (issuer id, key id and
the .p8 private key). Without one, verification still works; only this
self-refresh is unavailable.
*/

// AppleServerAPIConfig configures the App Store Server API client.
type AppleServerAPIConfig struct {
	IssuerID string
	KeyID    string
	// PrivateKeyPEM is the .p8 file's contents.
	PrivateKeyPEM []byte
	// Verifier checks the signed payloads the API returns (and supplies the
	// bundle id).
	Verifier *AppleVerifier
	// HTTPClient overrides the client; BaseURL and SandboxBaseURL override
	// Apple's hosts (tests).
	HTTPClient     *http.Client
	BaseURL        string
	SandboxBaseURL string
}

// AppleServerAPI reads subscription state from Apple.
type AppleServerAPI struct {
	cfg  AppleServerAPIConfig
	key  *ecdsa.PrivateKey
	http *http.Client
}

// NewAppleServerAPI builds the client.
func NewAppleServerAPI(cfg AppleServerAPIConfig) (*AppleServerAPI, error) {
	if cfg.IssuerID == "" || cfg.KeyID == "" || len(cfg.PrivateKeyPEM) == 0 {
		return nil, fmt.Errorf("cashier/iap/apple: the App Store Server API needs an issuer id, key id and private key")
	}
	if cfg.Verifier == nil {
		return nil, fmt.Errorf("cashier/iap/apple: the App Store Server API needs a verifier")
	}
	block, _ := pem.Decode(cfg.PrivateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("cashier/iap/apple: the In-App Purchase key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cashier/iap/apple: the In-App Purchase key is not a PKCS#8 key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("cashier/iap/apple: the In-App Purchase key is not an EC key")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.storekit.itunes.apple.com"
	}
	if cfg.SandboxBaseURL == "" {
		cfg.SandboxBaseURL = "https://api.storekit-sandbox.itunes.apple.com"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &AppleServerAPI{cfg: cfg, key: key, http: client}, nil
}

// token signs the short-lived bearer token Apple expects.
func (a *AppleServerAPI) token() (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": a.cfg.IssuerID,
		"iat": now.Unix(),
		"exp": now.Add(20 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
		"bid": a.cfg.Verifier.bundleID,
	})
	t.Header["kid"] = a.cfg.KeyID
	return t.SignedString(a.key)
}

type appleStatusResponse struct {
	Data []struct {
		LastTransactions []struct {
			OriginalTransactionID string `json:"originalTransactionId"`
			SignedTransactionInfo string `json:"signedTransactionInfo"`
			SignedRenewalInfo     string `json:"signedRenewalInfo"`
		} `json:"lastTransactions"`
	} `json:"data"`
}

// SubscriptionStatus returns the current state of the subscription that
// originalTransactionID started, trying production first and then sandbox
// (Apple answers 404 on the wrong environment).
func (a *AppleServerAPI) SubscriptionStatus(ctx context.Context, originalTransactionID string) (*contracts.IAPEntitlement, error) {
	path := "/inApps/v1/subscriptions/" + url.PathEscape(originalTransactionID)
	var resp appleStatusResponse
	found, err := a.get(ctx, a.cfg.BaseURL+path, &resp)
	if err == nil && !found && a.cfg.Verifier.allowSandbox {
		found, err = a.get(ctx, a.cfg.SandboxBaseURL+path, &resp)
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("cashier/iap/apple: Apple has no subscription %q", originalTransactionID)
	}
	for _, group := range resp.Data {
		for _, last := range group.LastTransactions {
			if last.OriginalTransactionID != originalTransactionID {
				continue
			}
			var tx appleTransaction
			if err := a.cfg.Verifier.verifyJWS(last.SignedTransactionInfo, &tx); err != nil {
				return nil, err
			}
			if tx.BundleID != a.cfg.Verifier.bundleID {
				return nil, fmt.Errorf("cashier/iap/apple: status is for bundle %q", tx.BundleID)
			}
			var renewal *appleRenewalInfo
			if last.SignedRenewalInfo != "" {
				var ri appleRenewalInfo
				if err := a.cfg.Verifier.verifyJWS(last.SignedRenewalInfo, &ri); err != nil {
					return nil, err
				}
				renewal = &ri
			}
			return appleEntitlement(tx, renewal), nil
		}
	}
	return nil, fmt.Errorf("cashier/iap/apple: status response did not include %q", originalTransactionID)
}

// get calls the API; found is false on a 404.
func (a *AppleServerAPI) get(ctx context.Context, u string, out any) (bool, error) {
	tok, err := a.token()
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("cashier/iap/apple: App Store Server API error %d: %s", resp.StatusCode, string(raw))
	}
	return true, json.Unmarshal(raw, out)
}
