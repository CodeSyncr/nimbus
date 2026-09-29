package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAppleServerAPI_SubscriptionStatus(t *testing.T) {
	chain := newAppleTestChain(t)
	verifier, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM, AllowSandbox: true})

	signingKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(signingKey)
	p8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	txJWS := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx-7", "originalTransactionId": "otx-7", "bundleId": "com.example.app",
		"productId": "pro.monthly", "expiresDate": time.Now().Add(time.Hour).UnixMilli(), "environment": "Sandbox",
	})
	renewal := chain.sign(t, jwt.MapClaims{"originalTransactionId": "otx-7", "autoRenewStatus": 1})

	var prodHits, sandboxHits int
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/prod/") {
			prodHits++
			http.NotFound(w, r) // a sandbox subscription is unknown in production
			return
		}
		sandboxHits++
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{
			"lastTransactions": []any{map[string]any{
				"originalTransactionId": "otx-7", "signedTransactionInfo": txJWS, "signedRenewalInfo": renewal,
			}},
		}}})
	}))
	defer srv.Close()

	api, err := NewAppleServerAPI(AppleServerAPIConfig{
		IssuerID: "issuer", KeyID: "KEY123", PrivateKeyPEM: p8, Verifier: verifier,
		BaseURL: srv.URL + "/prod", SandboxBaseURL: srv.URL + "/sandbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	ent, err := api.SubscriptionStatus(context.Background(), "otx-7")
	if err != nil {
		t.Fatalf("SubscriptionStatus: %v", err)
	}
	if prodHits != 1 || sandboxHits != 1 {
		t.Errorf("hits prod=%d sandbox=%d, want production then sandbox", prodHits, sandboxHits)
	}
	if !ent.Active || !ent.AutoRenewing || ent.TransactionID != "tx-7" {
		t.Errorf("entitlement = %+v", ent)
	}

	// The bearer token is ES256-signed with the key and names the bundle.
	parsed, err := jwt.Parse(strings.TrimPrefix(auth, "Bearer "), func(*jwt.Token) (any, error) { return &signingKey.PublicKey, nil })
	if err != nil {
		t.Fatalf("bearer token: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["bid"] != "com.example.app" || claims["aud"] != "appstoreconnect-v1" || parsed.Header["kid"] != "KEY123" {
		t.Errorf("token claims = %v header = %v", claims, parsed.Header)
	}
}
