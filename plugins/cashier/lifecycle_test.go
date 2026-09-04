package cashier

import (
	"testing"
	"time"
)

// harness builds a lifecycle over an in-memory paywall with a two-product
// catalogue: pro and team both unlock "premium", plus their own slug.
func harness(t *testing.T) (*Lifecycle, *Cashier, *[]SubscriberEvent) {
	t.Helper()
	catalog := NewCatalog()
	catalog.RegisterProduct(Product{ID: "pro", Name: "Pro", Amount: 1000, Currency: "INR", PeriodMonths: 1, Entitlements: []string{"premium", "pro"}})
	catalog.RegisterProduct(Product{ID: "team", Name: "Team", Amount: 3000, Currency: "INR", PeriodMonths: 1, Entitlements: []string{"premium", "team"}})

	paywall := NewPaywall(nil)
	events := &[]SubscriberEvent{}
	lc := NewLifecycle(catalog, paywall, time.Hour, func(e SubscriberEvent) { *events = append(*events, e) })
	cash := &Cashier{Paywall: paywall, Catalog: catalog, Lifecycle: lc}
	return lc, cash, events
}

func lastEvent(t *testing.T, events *[]SubscriberEvent) SubscriberEvent {
	t.Helper()
	if len(*events) == 0 {
		t.Fatal("no events emitted")
	}
	return (*events)[len(*events)-1]
}

func TestInitialPurchaseGrantsMappedEntitlements(t *testing.T) {
	lc, cash, events := harness(t)

	evt, err := lc.RecordPurchase("u1", "pro", time.Now().Add(30*24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventInitialPurchase {
		t.Fatalf("want initial_purchase, got %s", evt.Type)
	}
	if !cash.HasAccess("u1", "premium") || !cash.HasAccess("u1", "pro") {
		t.Fatal("purchase should unlock every mapped entitlement")
	}
	if cash.HasAccess("u1", "team") {
		t.Fatal("unpurchased product's exclusive entitlement must stay locked")
	}
	if got := lastEvent(t, events); got.PeriodType != PeriodNormal {
		t.Fatalf("default period type should be normal, got %s", got.PeriodType)
	}
}

func TestSecondPurchaseOfSameProductIsRenewal(t *testing.T) {
	lc, _, _ := harness(t)

	if _, err := lc.RecordPurchase("u1", "pro", time.Now().Add(24*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	evt, err := lc.RecordPurchase("u1", "pro", time.Now().Add(48*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventRenewal {
		t.Fatalf("want renewal, got %s", evt.Type)
	}
}

func TestSwitchingPaidProductsIsProductChange(t *testing.T) {
	lc, cash, _ := harness(t)

	if _, err := lc.RecordPurchase("u1", "pro", time.Now().Add(24*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	evt, err := lc.RecordPurchase("u1", "team", time.Now().Add(24*time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventProductChange || evt.OldProductID != "pro" {
		t.Fatalf("want product_change from pro, got %s from %q", evt.Type, evt.OldProductID)
	}
	if cash.HasAccess("u1", "pro") {
		t.Fatal("old product's exclusive entitlement must be revoked")
	}
	if !cash.HasAccess("u1", "premium") || !cash.HasAccess("u1", "team") {
		t.Fatal("shared and new entitlements must survive the change")
	}
}

func TestCancellationKeepsAccessAndStopsRenewal(t *testing.T) {
	lc, cash, _ := harness(t)

	if _, err := lc.RecordPurchase("u1", "pro", time.Now().Add(24*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	evt, err := lc.RecordCancellation("u1", "pro", "")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventCancellation || evt.Reason != ReasonUnsubscribe {
		t.Fatalf("want cancellation/unsubscribe, got %s/%s", evt.Type, evt.Reason)
	}
	if !cash.HasAccess("u1", "premium") {
		t.Fatal("cancelling must not cut access before the period ends")
	}
	info := cash.CustomerInfo("u1")
	if e := info.Entitlements["premium"]; e.WillRenew {
		t.Fatal("cancelled entitlement must report will_renew=false")
	}
}

func TestUncancellationRestoresRenewal(t *testing.T) {
	lc, cash, _ := harness(t)

	_, _ = lc.RecordPurchase("u1", "pro", time.Now().Add(24*time.Hour), "")
	_, _ = lc.RecordCancellation("u1", "pro", "")
	evt, err := lc.RecordUncancellation("u1", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventUncancellation {
		t.Fatalf("want uncancellation, got %s", evt.Type)
	}
	if e := cash.CustomerInfo("u1").Entitlements["premium"]; !e.WillRenew {
		t.Fatal("uncancellation must restore will_renew")
	}
}

func TestBillingIssueOpensGracePeriod(t *testing.T) {
	lc, cash, _ := harness(t)

	// Period about to lapse: a failed charge should extend it by grace.
	if _, err := lc.RecordPurchase("u1", "pro", time.Now().Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	evt, err := lc.RecordBillingIssue("u1", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if evt.Type != EventBillingIssue || evt.PeriodType != PeriodGrace {
		t.Fatalf("want billing_issue in grace, got %s/%s", evt.Type, evt.PeriodType)
	}
	info := cash.CustomerInfo("u1")
	prem := info.Entitlements["premium"]
	if !prem.Active || prem.PeriodType != PeriodGrace {
		t.Fatal("grace must keep the entitlement active and mark the period")
	}
	if prem.ExpiresAt == nil || time.Until(*prem.ExpiresAt) < 30*time.Minute {
		t.Fatal("grace must extend the expiry toward now+grace")
	}
}

func TestBillingIssueDoesNotShortenLongCoverage(t *testing.T) {
	lc, cash, _ := harness(t)

	far := time.Now().Add(30 * 24 * time.Hour)
	_, _ = lc.RecordPurchase("u1", "pro", far, "")
	_, _ = lc.RecordBillingIssue("u1", "pro")
	prem := cash.CustomerInfo("u1").Entitlements["premium"]
	if prem.ExpiresAt == nil || prem.ExpiresAt.Before(far.Add(-time.Minute)) {
		t.Fatal("grace must never pull an expiry earlier")
	}
}

func TestExpirationRevokesAndEmits(t *testing.T) {
	lc, cash, events := harness(t)

	_, _ = lc.RecordPurchase("u1", "pro", time.Now().Add(24*time.Hour), "")
	if _, err := lc.RecordExpiration("u1", "pro", ReasonBillingError); err != nil {
		t.Fatal(err)
	}
	if cash.HasAccess("u1", "premium") || cash.HasAccess("u1", "pro") {
		t.Fatal("expiration must revoke every mapped entitlement")
	}
	if got := lastEvent(t, events); got.Type != EventExpiration || got.Reason != ReasonBillingError {
		t.Fatalf("want expiration/billing_error, got %s/%s", got.Type, got.Reason)
	}
}

func TestPromotionalGrantAndRevoke(t *testing.T) {
	lc, cash, _ := harness(t)

	if _, err := lc.GrantPromotional("u1", "premium", time.Now().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	info := cash.CustomerInfo("u1")
	prem := info.Entitlements["premium"]
	if !prem.Active || prem.Source != SourcePromotional || prem.PeriodType != PeriodPromotional {
		t.Fatalf("promo grant state wrong: %+v", prem)
	}
	if prem.WillRenew {
		t.Fatal("promotional entitlements never renew")
	}
	if _, err := lc.RevokePromotional("u1", "premium"); err != nil {
		t.Fatal(err)
	}
	if cash.HasAccess("u1", "premium") {
		t.Fatal("revoked promo must not grant access")
	}
}

func TestNonRenewingPurchase(t *testing.T) {
	lc, cash, events := harness(t)

	if _, err := lc.RecordNonRenewingPurchase("u1", "pro", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !cash.HasAccess("u1", "premium") {
		t.Fatal("lifetime purchase must grant access")
	}
	got := lastEvent(t, events)
	if got.Type != EventNonRenewingPurchase || got.ExpiresAt != nil {
		t.Fatalf("want non_renewing_purchase without expiry, got %s exp=%v", got.Type, got.ExpiresAt)
	}
	if cash.CustomerInfo("u1").Entitlements["premium"].WillRenew {
		t.Fatal("one-off purchases must not report will_renew")
	}
}

func TestTransferMovesEntitlements(t *testing.T) {
	lc, cash, events := harness(t)

	_, _ = lc.RecordPurchase("anon-device", "pro", time.Now().Add(24*time.Hour), "")
	if _, err := lc.Transfer("anon-device", "u1"); err != nil {
		t.Fatal(err)
	}
	if cash.HasAccess("anon-device", "premium") {
		t.Fatal("transfer must revoke the source subject")
	}
	if !cash.HasAccess("u1", "premium") || !cash.HasAccess("u1", "pro") {
		t.Fatal("transfer must grant the destination subject")
	}
	got := lastEvent(t, events)
	if got.Type != EventTransfer || got.FromSubject != "anon-device" || got.Subject != "u1" {
		t.Fatalf("transfer event wrong: %+v", got)
	}
}

func TestCustomerInfoAggregates(t *testing.T) {
	lc, cash, _ := harness(t)

	exp := time.Now().Add(24 * time.Hour)
	_, _ = lc.RecordPurchase("u1", "pro", exp, PeriodTrial)

	info := cash.CustomerInfo("u1")
	if !info.HasEntitlement("premium") || !info.HasEntitlement("pro") {
		t.Fatalf("active entitlements missing: %+v", info.ActiveEntitlementIDs)
	}
	if len(info.ActiveProductIDs) != 1 || info.ActiveProductIDs[0] != "pro" {
		t.Fatalf("want active product [pro], got %v", info.ActiveProductIDs)
	}
	if info.Entitlements["premium"].PeriodType != PeriodTrial {
		t.Fatal("period type must flow through to customer info")
	}
	if info.LatestExpiresAt == nil || info.LatestExpiresAt.Sub(exp) > time.Second || exp.Sub(*info.LatestExpiresAt) > time.Second {
		t.Fatalf("latest expiry wrong: %v", info.LatestExpiresAt)
	}
}

func TestOfferingsCatalog(t *testing.T) {
	catalog := NewCatalog()
	catalog.RegisterProduct(Product{ID: "pro_monthly", Entitlements: []string{"premium"}})
	catalog.RegisterProduct(Product{ID: "pro_annual", Entitlements: []string{"premium"}})
	catalog.RegisterOffering(Offering{ID: "default", Packages: []Package{
		{ID: "monthly", ProductID: "pro_monthly"},
		{ID: "annual", ProductID: "pro_annual"},
	}})

	current, ok := catalog.CurrentOffering()
	if !ok || current.ID != "default" {
		t.Fatal("first registered offering must become current")
	}
	if got := catalog.ProductsUnlocking("premium"); len(got) != 2 {
		t.Fatalf("want 2 products unlocking premium, got %d", len(got))
	}
	// Unregistered products fall back to themselves as the entitlement.
	if got := catalog.EntitlementsFor("legacy-plan"); len(got) != 1 || got[0] != "legacy-plan" {
		t.Fatalf("fallback entitlement mapping broken: %v", got)
	}
}

func TestPluginWithoutCloudKeyIsPaymentsOnly(t *testing.T) {
	t.Setenv("CASHIER_CLOUD_KEY", "")
	p := NewPlugin(Config{
		Products:  []Product{{ID: "pro", Entitlements: []string{"premium"}}},
		Offerings: []Offering{{ID: "default", Packages: []Package{{ID: "m", ProductID: "pro"}}}},
	})
	if p.Cashier.CloudEnabled() {
		t.Fatal("no cloud key must leave the subscription suite disabled")
	}
	if p.Cashier.Catalog != nil || p.Cashier.Lifecycle != nil {
		t.Fatal("catalogue and lifecycle are Cashier Cloud features")
	}
	if info := p.Cashier.CustomerInfo("u1"); len(info.Entitlements) != 0 {
		t.Fatal("customer management is a Cashier Cloud feature")
	}
}

func TestCloudKeyActivatesSubscriptionSuite(t *testing.T) {
	p := NewPlugin(Config{
		CloudKey:  "cshr_live_test",
		Products:  []Product{{ID: "pro", Entitlements: []string{"premium"}}},
		Offerings: []Offering{{ID: "default", Packages: []Package{{ID: "m", ProductID: "pro"}}}},
	})
	if !p.Cashier.CloudEnabled() {
		t.Fatal("a cloud key must activate the subscription suite")
	}
	if got := p.Cashier.Catalog.EntitlementsFor("pro"); len(got) != 1 || got[0] != "premium" {
		t.Fatalf("catalogue not seeded: %v", got)
	}
	if _, ok := p.Cashier.Catalog.CurrentOffering(); !ok {
		t.Fatal("offerings not seeded")
	}
}
