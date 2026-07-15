package gateways

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// PayUConfig configures the PayU (India) gateway. PaymentURL defaults to the
// production endpoint; use https://test.payu.in/_payment for sandbox.
type PayUConfig struct {
	MerchantKey  string
	MerchantSalt string
	PaymentURL   string
}

// PayUGateway implements contracts.PaymentGateway for PayU India. PayU is a
// hash-signed browser-form flow: CreateCharge returns the signed form fields +
// action URL (Charge.Raw / RedirectURL) which the frontend auto-submits; PayU
// posts the result back, verified with VerifyWebhook (reverse hash).
type PayUGateway struct {
	cfg PayUConfig
}

// NewPayU builds the PayU gateway.
func NewPayU(cfg PayUConfig) *PayUGateway {
	if cfg.PaymentURL == "" {
		cfg.PaymentURL = "https://secure.payu.in/_payment"
	}
	return &PayUGateway{cfg: cfg}
}

func (p *PayUGateway) Name() string { return "payu" }

// CreateCharge builds the signed PayU request. It does not call PayU — the
// returned Charge.Raw holds the form fields the browser POSTs to RedirectURL.
func (p *PayUGateway) CreateCharge(ctx context.Context, cp contracts.ChargeParams) (*contracts.Charge, error) {
	if p.cfg.MerchantKey == "" || p.cfg.MerchantSalt == "" {
		return nil, fmt.Errorf("cashier/payu: MerchantKey/MerchantSalt not set")
	}
	if cp.Amount <= 0 {
		return nil, fmt.Errorf("cashier/payu: Amount (in paise) is required")
	}
	txnid := firstNonEmpty(cp.Reference, "txn_"+randomHex(8))
	amount := fmt.Sprintf("%.2f", float64(cp.Amount)/100) // PayU amount is in rupees
	productinfo := firstNonEmpty(cp.Metadata["productinfo"], "Order")
	firstname := firstNonEmpty(cp.Metadata["firstname"], "Customer")
	email := cp.CustomerEmail
	udf := [10]string{}

	hash := p.requestHash(txnid, amount, productinfo, firstname, email, udf)

	fields := map[string]any{
		"key":         p.cfg.MerchantKey,
		"txnid":       txnid,
		"amount":      amount,
		"productinfo": productinfo,
		"firstname":   firstname,
		"email":       email,
		"phone":       cp.Metadata["phone"],
		"surl":        cp.SuccessURL,
		"furl":        cp.CancelURL,
		"hash":        hash,
	}
	return &contracts.Charge{
		Gateway:     p.Name(),
		ID:          txnid,
		RedirectURL: p.cfg.PaymentURL,
		Amount:      cp.Amount,
		Currency:    firstNonEmpty(cp.Currency, "INR"),
		Raw:         fields,
	}, nil
}

// VerifyPayment verifies a PayU response supplied as PaymentProof (OrderID=txnid,
// Signature=posted hash). Prefer VerifyWebhook when you have the full form.
func (p *PayUGateway) VerifyPayment(ctx context.Context, proof contracts.PaymentProof) (bool, error) {
	return false, contracts.ErrUnsupported // PayU verification needs the full posted form — use VerifyWebhook
}

// VerifyWebhook verifies a PayU callback. The payload is the raw form-encoded
// body (application/x-www-form-urlencoded); the reverse hash is recomputed and
// compared to the posted "hash".
func (p *PayUGateway) VerifyWebhook(payload []byte, headers http.Header) (*contracts.WebhookEvent, error) {
	if p.cfg.MerchantSalt == "" {
		return nil, fmt.Errorf("cashier/payu: MerchantSalt not set")
	}
	form, err := url.ParseQuery(string(payload))
	if err != nil {
		return nil, fmt.Errorf("cashier/payu: parse form: %w", err)
	}
	var udf [10]string
	for i := 0; i < 10; i++ {
		udf[i] = form.Get(fmt.Sprintf("udf%d", i+1))
	}
	expected := p.responseHash(
		form.Get("status"),
		form.Get("txnid"),
		form.Get("amount"),
		form.Get("productinfo"),
		form.Get("firstname"),
		form.Get("email"),
		udf,
	)
	if !strings.EqualFold(expected, form.Get("hash")) {
		return nil, fmt.Errorf("cashier/payu: hash verification failed")
	}
	return &contracts.WebhookEvent{Gateway: p.Name(), Type: form.Get("status"), ID: form.Get("txnid"), Raw: payload}, nil
}

// requestHash: sha512(key|txnid|amount|productinfo|firstname|email|udf1..udf10|salt)
func (p *PayUGateway) requestHash(txnid, amount, productinfo, firstname, email string, udf [10]string) string {
	parts := append([]string{p.cfg.MerchantKey, txnid, amount, productinfo, firstname, email}, udf[:]...)
	parts = append(parts, p.cfg.MerchantSalt)
	return sha512hex(strings.Join(parts, "|"))
}

// responseHash: sha512(salt|status|udf10..udf1|email|firstname|productinfo|amount|txnid|key)
func (p *PayUGateway) responseHash(status, txnid, amount, productinfo, firstname, email string, udf [10]string) string {
	parts := []string{p.cfg.MerchantSalt, status}
	for i := 9; i >= 0; i-- {
		parts = append(parts, udf[i])
	}
	parts = append(parts, email, firstname, productinfo, amount, txnid, p.cfg.MerchantKey)
	return sha512hex(strings.Join(parts, "|"))
}

func sha512hex(s string) string {
	sum := sha512.Sum512([]byte(s))
	return hex.EncodeToString(sum[:])
}
