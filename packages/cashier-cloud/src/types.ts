/**
 * The types the SDK reads off the wire.
 *
 * These describe what Cloud is authoritative for: what a paywall should sell
 * (products, offerings), what a subscriber owns (entitlements, CustomerInfo),
 * and how that changed (subscriber events, which are also the shape of the
 * webhooks Cloud sends your backend).
 *
 * Money is always in the smallest currency unit — paise for INR, cents for USD.
 * Where a zero `time.Time` would mean "never" in Go, this uses `null`.
 */

// ── Catalogue ─────────────────────────────────────────────────────

/** One purchasable SKU and the entitlements it unlocks. */
export interface Product {
  id: string
  name?: string
  amount?: number
  currency?: string
  /** Billing period; 0 = non-renewing / lifetime. */
  periodMonths?: number
  trialDays?: number
  /** Entitlement ids this product unlocks. */
  entitlements?: string[]
}

/** One product slot inside an offering, e.g. "monthly" / "annual". */
export interface Package {
  id: string
  productId: string
}

/** A named group of packages a paywall presents. */
export interface Offering {
  id: string
  packages: Package[]
  metadata?: Record<string, string>
}

/** One package with its product resolved — what the offerings endpoint serves. */
export interface ResolvedPackage {
  id: string
  product: Product
}

/** The paywall payload: the current offering with every product resolved. */
export interface OfferingsResponse {
  currentOffering: string
  metadata?: Record<string, string>
  packages: ResolvedPackage[]
  /** The one-month package, if the offering has one. */
  monthly?: ResolvedPackage
  /** The twelve-month package, if the offering has one. */
  annual?: ResolvedPackage
  /** The non-renewing paid package, if the offering has one. */
  lifetime?: ResolvedPackage
}

// ── Entitlements ──────────────────────────────────────────────────

export type PeriodType = 'trial' | 'intro' | 'normal' | 'promotional' | 'grace'
export type EntitlementSource = 'purchase' | 'promotional'

/**
 * Grants a subject access to an entitlement until `expiresAt`.
 * A `null` expiry never lapses.
 */
export interface Entitlement {
  subject: string
  /** The entitlement id being granted. */
  plan: string
  expiresAt: Date | null
  /** The product whose purchase granted this (empty for promos). */
  productId?: string
  periodType?: PeriodType
  source?: EntitlementSource
  /** False once cancelled or paused — access persists to expiry regardless. */
  willRenew?: boolean
}

/** One entitlement's state inside CustomerInfo. */
export interface EntitlementInfo {
  id: string
  active: boolean
  willRenew: boolean
  periodType?: PeriodType
  productId?: string
  source?: EntitlementSource
  expiresAt?: Date | null
}

/** One aggregate answering "what does this subscriber have right now?". */
export interface CustomerInfo {
  subject: string
  requestedAt: Date
  /** Every known entitlement, keyed by id — active or not. */
  entitlements: Record<string, EntitlementInfo>
  /** The ids currently granting access, sorted. */
  activeEntitlementIds: string[]
  /** The products behind active entitlements, sorted. */
  activeProductIds: string[]
  /** Store subscriptions currently granting access. */
  activeSubscriptions?: Subscription[]
  /** Furthest-out expiry across active entitlements; null when one never expires. */
  latestExpiresAt?: Date | null
}

/** Reports whether the aggregate holds an active entitlement. */
export function hasEntitlement(info: CustomerInfo | null | undefined, id: string): boolean {
  const e = info?.entitlements?.[id]
  return !!e && e.active
}

// ── Subscriptions ─────────────────────────────────────────────────

/**
 * The canonical view of a subscription, mapped from each store's own
 * vocabulary so access checks read the same across Apple, Google and Stripe.
 */
export type SubscriptionStatus =
  | 'active'
  | 'trialing'
  | 'past_due'
  | 'paused'
  | 'canceled'
  | 'expired'
  | 'pending'
  | 'unknown'

/** Reports whether a status currently entitles the subject to access. */
export function statusGrants(s: SubscriptionStatus): boolean {
  return s === 'active' || s === 'trialing'
}

/** A store subscription reduced to what Cloud mirrors. */
export interface Subscription {
  /** "apple" | "google" | "stripe" */
  store: string
  id: string
  status: SubscriptionStatus
  productId?: string
  subject?: string
  currentPeriodEnd?: Date | null
  trialEnd?: Date | null
  canceledAt?: Date | null
  endedAt?: Date | null
  raw?: Record<string, unknown>
}

// ── Subscriber events ─────────────────────────────────────────────

/** Subscriber event types (RevenueCat parity). */
export type SubscriberEventType =
  | 'initial_purchase'
  | 'renewal'
  | 'non_renewing_purchase'
  | 'cancellation'
  | 'uncancellation'
  | 'expiration'
  | 'billing_issue'
  | 'product_change'
  | 'subscription_paused'
  | 'subscription_extended'
  | 'promotional_grant'
  | 'promotional_revoke'
  | 'transfer'

/** Cancellation / expiration reasons. */
export type SubscriberReason =
  | 'unsubscribe'
  | 'billing_error'
  | 'developer_initiated'
  | 'price_increase'
  | 'customer_support'
  | 'unknown'

/** One canonical moment in a subscriber's life. Also the outbound webhook payload. */
export interface SubscriberEvent {
  type: SubscriberEventType
  subject: string
  productId?: string
  /** product_change: the plan being left. */
  oldProductId?: string
  /** transfer: the subject the entitlements came from. */
  fromSubject?: string
  entitlementIds?: string[]
  periodType?: PeriodType
  reason?: SubscriberReason
  expiresAt?: Date | null
  at: Date
}
