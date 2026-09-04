package iap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// googleTestSA builds a throwaway service-account JSON pointing its token URI
// at the test server, so the OAuth2 exchange and the API call are both mocked.
func googleTestSA(t *testing.T, tokenURI string) []byte {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	sa, _ := json.Marshal(map[string]string{
		"client_email": "svc@test.iam.gserviceaccount.com",
		"private_key":  string(keyPEM),
		"token_uri":    tokenURI,
	})
	return sa
}

func TestGoogle_VerifySubscription(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			_, _ = w.Write([]byte(`{"access_token":"ya29.test","expires_in":3600}`))
		case strings.Contains(r.URL.Path, "/subscriptionsv2/tokens/"):
			sawAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{
				"subscriptionState":"SUBSCRIPTION_STATE_ACTIVE",
				"latestOrderId":"GPA.1",
				"lineItems":[{"productId":"pro.monthly","expiryTime":"2099-01-01T00:00:00Z"}]
			}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, 400)
		}
	}))
	defer srv.Close()

	// Point both the token URI and the API host at the test server: the API
	// host is fixed in code, so route by path on one server and only assert on
	// the token exchange plus mapping.
	v, err := NewGoogle(GoogleConfig{PackageName: "com.example.app", ServiceAccountJSON: googleTestSA(t, srv.URL+"/token")})
	if err != nil {
		t.Fatal(err)
	}
	v.http = srv.Client()
	// Redirect the API base by overriding the transport to the test server.
	v.http = redirectingClient(srv.URL)

	ent, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{
		Platform: contracts.PlatformGoogle, ProductID: "pro.monthly", Subject: "u1",
		Token: "purchase-token", Subscription: true,
	})
	if err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
	if !ent.Active || !ent.Subscription {
		t.Errorf("expected active subscription: %+v", ent)
	}
	if ent.ProductID != "pro.monthly" {
		t.Errorf("product id = %q", ent.ProductID)
	}
	if ent.OriginalTransactionID != "GPA.1" {
		t.Errorf("order id not carried: %q", ent.OriginalTransactionID)
	}
	if ent.ExpiresAt == nil {
		t.Error("expiry not parsed")
	}
	if sawAuth != "Bearer ya29.test" {
		t.Errorf("API call was not authenticated with the exchanged token: %q", sawAuth)
	}
}

// A product bought under a different id than the client claims must be rejected.
func TestGoogle_RejectsProductMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"subscriptionState":"SUBSCRIPTION_STATE_ACTIVE","latestOrderId":"o","lineItems":[{"productId":"actual.product","expiryTime":"2099-01-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()

	v, _ := NewGoogle(GoogleConfig{PackageName: "com.example.app", ServiceAccountJSON: googleTestSA(t, srv.URL+"/token")})
	v.http = redirectingClient(srv.URL)

	_, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{
		Platform: contracts.PlatformGoogle, ProductID: "claimed.product", Token: "tok", Subscription: true,
	})
	if err == nil {
		t.Fatal("a product mismatch was accepted")
	}
}

func TestGoogle_ParsesNotification(t *testing.T) {
	v, _ := NewGoogle(GoogleConfig{PackageName: "com.example.app", ServiceAccountJSON: googleTestSA(t, "http://x/token")})

	inner, _ := json.Marshal(map[string]any{
		"packageName": "com.example.app",
		"subscriptionNotification": map[string]any{
			"notificationType": 2, "purchaseToken": "ptok", "subscriptionId": "pro.monthly",
		},
	})
	rtdn, _ := json.Marshal(map[string]any{
		"message": map[string]any{"data": base64.StdEncoding.EncodeToString(inner)},
	})

	note, err := v.ParseNotification(rtdn)
	if err != nil {
		t.Fatalf("ParseNotification: %v", err)
	}
	if note.Type != "renewed" {
		t.Errorf("canonical type = %q, want renewed", note.Type)
	}
	if note.OriginalTransactionID != "ptok" {
		t.Errorf("purchase token not carried: %q", note.OriginalTransactionID)
	}
}

func TestGoogle_RequiresServiceAccount(t *testing.T) {
	if _, err := NewGoogle(GoogleConfig{PackageName: "x", ServiceAccountJSON: []byte("{}")}); err == nil {
		t.Fatal("verifier built with an empty service account")
	}
}

// redirectingClient sends every request to base, preserving the path — so the
// code's fixed androidpublisher host lands on the test server.
func redirectingClient(base string) *http.Client {
	return &http.Client{Transport: redirectTransport{base: base}}
}

type redirectTransport struct{ base string }

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target := rt.base + req.URL.Path
	if req.URL.RawQuery != "" {
		target += "?" + req.URL.RawQuery
	}
	// Keep the token-exchange host untouched (it already points at the server).
	if strings.HasSuffix(req.URL.Path, "/token") {
		target = req.URL.String()
	}
	newReq, _ := http.NewRequestWithContext(req.Context(), req.Method, target, req.Body)
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}
