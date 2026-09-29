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

// appleTestChain builds a throwaway root→intermediate→leaf ECDSA chain shaped
// like Apple's (the intermediate and leaf carry Apple's receipt OIDs) and
// returns the PEM of the root plus a signer that mints StoreKit-style JWS
// tokens against the leaf. This lets the verifier be tested end to end without
// Apple's real certs.
type appleTestChain struct {
	rootPEM  []byte
	leafKey  *ecdsa.PrivateKey
	leafDER  []byte
	interDER []byte
	rootDER  []byte
}

type appleChainOpts struct {
	// skipLeafOID / skipInterOID drop Apple's marker extensions, to model a
	// developer certificate that chains to the same root.
	skipLeafOID, skipInterOID bool
}

func newAppleTestChain(t *testing.T) *appleTestChain {
	return newAppleTestChainWith(t, appleChainOpts{})
}

func newAppleTestChainWith(t *testing.T, o appleChainOpts) *appleTestChain {
	t.Helper()
	marker := func(oid []int, skip bool) []pkix.Extension {
		if skip {
			return nil
		}
		return []pkix.Extension{{Id: oid, Value: []byte{0x05, 0x00}}}
	}

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

	interKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	interTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Apple WWDR"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
		ExtraExtensions:       marker([]int{1, 2, 840, 113635, 100, 6, 2, 1}, o.skipInterOID),
	}
	interDER, _ := x509.CreateCertificate(rand.Reader, interTmpl, rootCert, &interKey.PublicKey, rootKey)
	interCert, _ := x509.ParseCertificate(interDER)

	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber:    big.NewInt(3),
		Subject:         pkix.Name{CommonName: "Test Apple Leaf"},
		NotBefore:       time.Now().Add(-time.Hour),
		NotAfter:        time.Now().Add(24 * time.Hour),
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtraExtensions: marker([]int{1, 2, 840, 113635, 100, 6, 11, 1}, o.skipLeafOID),
	}
	leafDER, _ := x509.CreateCertificate(rand.Reader, leafTmpl, interCert, &leafKey.PublicKey, interKey)

	return &appleTestChain{rootPEM: pemCert(rootDER), leafKey: leafKey, leafDER: leafDER, interDER: interDER, rootDER: rootDER}
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
		base64.StdEncoding.EncodeToString(c.interDER),
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

// Apple Root CA - G3 also anchors the intermediates that issue ordinary
// developer certificates. A JWS signed by one of those reaches the pinned root
// but lacks Apple's receipt markers, and must be refused.
func TestApple_RejectsDeveloperCertificateUnderAppleRoot(t *testing.T) {
	for name, opts := range map[string]appleChainOpts{
		"leaf without receipt OID":         {skipLeafOID: true},
		"intermediate without receipt OID": {skipInterOID: true},
	} {
		t.Run(name, func(t *testing.T) {
			chain := newAppleTestChainWith(t, opts)
			v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM})
			token := chain.sign(t, jwt.MapClaims{
				"transactionId": "tx", "bundleId": "com.example.app", "productId": "p", "environment": "Production",
			})
			if _, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple, Token: token}); err == nil {
				t.Fatal("a JWS without Apple's receipt markers was accepted")
			}
		})
	}
}

// The bundled root must be Apple Root CA - G3 and parse as a usable pool.
func TestApple_BundledRootIsG3(t *testing.T) {
	v, err := NewApple(AppleConfig{BundleID: "x", RootCertsPEM: []byte(AppleRootCAG3PEM)})
	if err != nil {
		t.Fatalf("bundled root rejected: %v", err)
	}
	if v == nil {
		t.Fatal("nil verifier")
	}
}

// Signed renewal info decides renewal intent and grace: a subscription past
// its period but inside Apple's grace period is still active, and a cancelled
// one is active but not renewing. Trials are read from the offer fields.
func TestApple_RenewalInfoGraceAndTrial(t *testing.T) {
	chain := newAppleTestChain(t)
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM})

	lapsed := time.Now().Add(-time.Hour).UnixMilli()
	token := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx-2", "originalTransactionId": "otx-2",
		"bundleId": "com.example.app", "productId": "pro.monthly",
		"expiresDate": lapsed, "environment": "Production",
		"offerType": 1, "offerDiscountType": "FREE_TRIAL", "price": 0,
	})
	renewal := chain.sign(t, jwt.MapClaims{
		"originalTransactionId": "otx-2", "autoRenewStatus": 0,
		"isInBillingRetryPeriod": true, "gracePeriodExpiresDate": time.Now().Add(48 * time.Hour).UnixMilli(),
	})
	ent, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{
		Platform: contracts.PlatformApple, Token: token, RenewalInfo: renewal,
	})
	if err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
	if !ent.Active || !ent.BillingIssue || ent.GraceExpiresAt == nil {
		t.Errorf("grace period not honoured: %+v", ent)
	}
	if ent.AutoRenewing {
		t.Error("autoRenewStatus 0 read as renewing")
	}
	if ent.PeriodType != "trial" {
		t.Errorf("period type = %q, want trial", ent.PeriodType)
	}

	// Renewal info from another subscription must not be paired with this one.
	other := chain.sign(t, jwt.MapClaims{"originalTransactionId": "otx-other", "autoRenewStatus": 1})
	if _, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{
		Platform: contracts.PlatformApple, Token: token, RenewalInfo: other,
	}); err == nil {
		t.Fatal("renewal info from another subscription was accepted")
	}
}

// A refunded (revoked) transaction never grants access.
func TestApple_RevokedIsInactive(t *testing.T) {
	chain := newAppleTestChain(t)
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM})
	token := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx", "originalTransactionId": "otx", "bundleId": "com.example.app",
		"productId": "lifetime", "environment": "Production", "revocationDate": time.Now().UnixMilli(),
	})
	ent, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if ent.Active || !ent.Revoked {
		t.Errorf("revoked purchase still active: %+v", ent)
	}
}

// A notification for another app's bundle is refused, and one for this app
// carries the verified purchase state.
func TestApple_NotificationBundleAndState(t *testing.T) {
	chain := newAppleTestChain(t)
	v, _ := NewApple(AppleConfig{BundleID: "com.example.app", RootCertsPEM: chain.rootPEM, AllowSandbox: true})

	txJWS := chain.sign(t, jwt.MapClaims{
		"transactionId": "tx", "originalTransactionId": "otx", "bundleId": "com.example.app",
		"productId": "pro.monthly", "expiresDate": time.Now().Add(time.Hour).UnixMilli(), "environment": "Sandbox",
	})
	renewal := chain.sign(t, jwt.MapClaims{"originalTransactionId": "otx", "autoRenewStatus": 0})
	note := func(bundle string) []byte {
		jws := chain.sign(t, jwt.MapClaims{
			"notificationType": "DID_CHANGE_RENEWAL_STATUS", "subtype": "AUTO_RENEW_DISABLED",
			"notificationUUID": "uuid-1",
			"data": map[string]any{
				"bundleId": bundle, "environment": "Sandbox",
				"signedTransactionInfo": txJWS, "signedRenewalInfo": renewal,
			},
		})
		b, _ := json.Marshal(map[string]string{"signedPayload": jws})
		return b
	}

	if _, err := v.ParseNotification(note("com.other.app")); err == nil {
		t.Fatal("a notification for another bundle was accepted")
	}
	n, err := v.ParseNotification(note("com.example.app"))
	if err != nil {
		t.Fatalf("ParseNotification: %v", err)
	}
	if n.Type != "canceled" || n.ID != "uuid-1" || n.Environment != "sandbox" {
		t.Errorf("notification = %+v", n)
	}
	if n.Entitlement == nil || !n.Entitlement.Active || n.Entitlement.AutoRenewing {
		t.Errorf("state not carried: %+v", n.Entitlement)
	}
}
