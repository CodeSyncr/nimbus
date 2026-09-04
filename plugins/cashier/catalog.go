package cashier

import "sync"

/*
Product → Entitlement catalogue, modeled on RevenueCat.

A Product is what a customer buys (a plan, a price, a store SKU). An
entitlement is what they unlock — a named level of access like "premium".
The two are deliberately decoupled: several products may unlock the same
entitlement (monthly and yearly Pro both grant "premium"), and one product may
unlock several. Access checks always ask about entitlements, never products,
so repricing or renaming a plan never touches gating code.

Offerings group products into the set a paywall should present (RevenueCat's
Offerings/Packages), so the client asks "what should I sell right now?" instead
of hard-coding product ids.
*/

// Product is one purchasable SKU and the entitlements it unlocks.
type Product struct {
	ID           string   `json:"id"`   // plan/price identifier ("pro", "price_123")
	Name         string   `json:"name"` // display name
	Amount       int64    `json:"amount"`
	Currency     string   `json:"currency"`
	PeriodMonths int      `json:"period_months"` // billing period; 0 = non-renewing / lifetime
	TrialDays    int      `json:"trial_days"`
	Entitlements []string `json:"entitlements"` // entitlement ids this product unlocks
}

// Paid reports whether the product requires a payment.
func (p Product) Paid() bool { return p.Amount > 0 }

// Package is one product slot inside an offering.
type Package struct {
	ID        string `json:"id"` // e.g. "monthly", "annual"
	ProductID string `json:"product_id"`
}

// Offering is a named group of packages a paywall presents.
type Offering struct {
	ID       string            `json:"id"`
	Packages []Package         `json:"packages"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Catalog is the registry of products, their entitlement mappings, and
// offerings. It is safe for concurrent use.
type Catalog struct {
	mu        sync.RWMutex
	products  map[string]Product
	order     []string
	offerings map[string]Offering
	current   string
}

// NewCatalog builds an empty catalogue.
func NewCatalog() *Catalog {
	return &Catalog{products: map[string]Product{}, offerings: map[string]Offering{}}
}

// RegisterProduct adds or replaces a product.
func (c *Catalog) RegisterProduct(p Product) {
	if p.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.products[p.ID]; !exists {
		c.order = append(c.order, p.ID)
	}
	c.products[p.ID] = p
}

// Product resolves a product id.
func (c *Catalog) Product(id string) (Product, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.products[id]
	return p, ok
}

// Products returns every registered product in registration order.
func (c *Catalog) Products() []Product {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Product, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.products[id])
	}
	return out
}

// EntitlementsFor returns the entitlement ids a product unlocks. An
// unregistered product (or one registered without entitlements) unlocks an
// entitlement named after itself, which keeps plan-slug based paywalls working
// unchanged.
func (c *Catalog) EntitlementsFor(productID string) []string {
	c.mu.RLock()
	p, ok := c.products[productID]
	c.mu.RUnlock()
	if !ok || len(p.Entitlements) == 0 {
		return []string{productID}
	}
	out := make([]string, len(p.Entitlements))
	copy(out, p.Entitlements)
	return out
}

// ProductsUnlocking returns every product that grants an entitlement.
func (c *Catalog) ProductsUnlocking(entitlementID string) []Product {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Product
	for _, id := range c.order {
		p := c.products[id]
		for _, e := range p.Entitlements {
			if e == entitlementID {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// RegisterOffering adds or replaces an offering. The first registered offering
// becomes current unless SetCurrentOffering has chosen one.
func (c *Catalog) RegisterOffering(o Offering) {
	if o.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offerings[o.ID] = o
	if c.current == "" {
		c.current = o.ID
	}
}

// SetCurrentOffering picks the offering paywalls should present.
func (c *Catalog) SetCurrentOffering(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = id
}

// CurrentOffering returns the offering paywalls should present now.
func (c *Catalog) CurrentOffering() (Offering, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	o, ok := c.offerings[c.current]
	return o, ok
}

// Offering resolves an offering id.
func (c *Catalog) Offering(id string) (Offering, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	o, ok := c.offerings[id]
	return o, ok
}

// Offerings returns every registered offering.
func (c *Catalog) Offerings() []Offering {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Offering, 0, len(c.offerings))
	for _, o := range c.offerings {
		out = append(out, o)
	}
	return out
}
