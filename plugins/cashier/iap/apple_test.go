package iap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
	"github.com/golang-jwt/jwt/v5"
)

// appleTestChain builds a throwaway root→leaf ECDSA chain and returns the PEM
// of the root plus a signer that mints StoreKit-style JWS tokens against the
// leaf. This lets the verifier be tested end to end without Apple's real certs.
type appleTestChain struct {
	rootPEM []byte
	leafKey *ecdsa.PrivateKey
	leafDER []byte
	rootDER []byte
}

func newAppleTestChain(t *testing.T) *appleTestChain {
	t.Helper()
	rootKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Apple Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, _ := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	rootCert, _ := x509.ParseCertificate(rootDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test Apple Leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTmpl, rootCert, &leafKey.PublicKey, rootKey)

	rootPEM := pemCert(rootDER)
	return &appleTestChain{rootPEM: rootPEM, leafKey: leafKey, leafDER: leafDER, rootDER: rootDER}
}

func pemCert(der []byte) []byte {
	return []byte("-----BEGIN CERTIFICATE-----\n" +
		wrap64(base64.StdEncoding.EncodeToString(der)) +
		"\n-----END CERTIFICATE-----\n")
}

func wrap64(s string) string {
	var out []byte
	for len(s) > 64 {
		out = append(out, s[:64]...)
		out = append(out, '\n')
		s = s[64:]
	}
	return string(append(out, s...))
}

// sign builds a JWS with the x5c chain header the verifier expects.
func (c *appleTestChain) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["x5c"] = []string{
		base64.StdEncoding.EncodeToString(c.leafDER),
		base64.StdEncoding.EncodeToString(c.rootDER),
	}
	s, err := tok.SignedString(c.leafKey)
	if err != nil {
		t.Fatalf("signing test JWS: %v", err)
	}
	return s
}

func TestApple_VerifyReceipt(t *testing.T) {
	chain := newAppleTestChain(t)
	v, err := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM})
	if err != nil {
		t.Fatal(err)
	}

	exp := time.Now().Add(720 * time.Hour).UnixMilli()
	token := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx-1", "originalTransactionId": "otx-1",
		"bundleId": "com.example.app", "productId": "pro.monthly",
		"expiresDate": exp, "environment": "Production",
	})

	ent, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{
		Platform: contracts.PlatformApple, ProductID: "pro.monthly", Subject: "u1", Token: token,
	})
	if err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
	if !ent.Active || !ent.Subscription {
		t.Errorf("entitlement should be an active subscription: %+v", ent)
	}
	if ent.OriginalTransactionID != "otx-1" {
		t.Errorf("original transaction id = %q", ent.OriginalTransactionID)
	}
	if ent.ExpiresAt == nil {
		t.Error("expiry not parsed")
	}
}

// A receipt for a different bundle must be rejected even though it is validly
// signed — otherwise one app's receipt authorises another.
func TestApple_RejectsWrongBundle(t *testing.T) {
	chain := newAppleTestChain(t)
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM})

	token := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx", "bundleId": "com.attacker.app", "productId": "p", "environment": "Production",
	})
	if _, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple, Token: token}); err == nil {
		t.Fatal("a receipt for the wrong bundle was accepted")
	}
}

// A sandbox receipt must not authorise a production entitlement.
func TestApple_RejectsSandboxInProduction(t *testing.T) {
	chain := newAppleTestChain(t)
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM}) // AllowSandbox false

	token := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx", "bundleId": "com.example.app", "productId": "p", "environment": "Sandbox",
	})
	if _, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple, Token: token}); err == nil {
		t.Fatal("a sandbox receipt was accepted on a production verifier")
	}
}

// A JWS signed by a chain that does not terminate at the pinned root is the
// core forgery case, and must be refused.
func TestApple_RejectsUntrustedChain(t *testing.T) {
	real := newAppleTestChain(t)
	attacker := newAppleTestChain(t)

	// Verifier trusts the real root; the token is signed by the attacker's.
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: real.rootPEM})
	token := attacker.sign(t, jwt.MapClaims{
		"transactionId": "tx", "bundleId": "com.example.app", "productId": "p", "environment": "Production",
	})
	if _, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple, Token: token}); err == nil {
		t.Fatal("a JWS from an untrusted certificate chain was accepted")
	}
}

// The verifier must refuse to build without a pinned root: there is nothing to
// anchor trust to otherwise.
func TestApple_RequiresRoot(t *testing.T) {
	if _, err := NewApple(AppleConfig{BundleID: "x"}); err == nil {
		t.Fatal("verifier built with no root certificate")
	}
}

func TestApple_ParsesNotification(t *testing.T) {
	chain := newAppleTestChain(t)
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM, AllowSandbox: true})

	txJWS := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx", "originalTransactionId": "otx-9",
		"bundleId": "com.example.app", "productId": "pro.monthly",
		"expiresDate": time.Now().Add(time.Hour).UnixMilli(), "environment": "Sandbox",
	})
	noteJWS := chain.sign(t, jwt.MapClaims{
		"notificationType": "DID_RENEW",
		"data":             map[string]any{"bundleId": "com.example.app", "signedTransactionInfo": txJWS},
	})
	payload, _ := json.Marshal(map[string]string{"signedPayload": noteJWS})

	note, err := v.ParseNotification(payload)
	if err != nil {
		t.Fatalf("ParseNotification: %v", err)
	}
	if note.Type != "renewed" {
		t.Errorf("canonical type = %q, want renewed", note.Type)
	}
	if note.OriginalTransactionID != "otx-9" {
		t.Errorf("nested transaction not read: %q", note.OriginalTransactionID)
	}
}
