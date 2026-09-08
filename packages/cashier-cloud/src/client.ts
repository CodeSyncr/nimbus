/**
 * The Cashier Cloud web SDK.
 *
 * This is the browser half — the counterpart of the iOS and Android SDKs. It
 * holds a **public** key, so everything it can do is safe to ship in a page:
 * read what the paywall should sell, read what the current subscriber owns,
 * and start a checkout. It cannot grant an entitlement; only Cloud can, and
 * only after a store or Stripe has confirmed the money moved.
 *
 *     const cashier = new CashierCloud({ apiKey: 'cshr_web_…' })
 *
 *     const { packages } = await cashier.offerings()
 *     await cashier.purchase(packages[0], { successUrl, cancelUrl })
 *
 *     const info = await cashier.customerInfo()
 *     if (info.entitlements.premium?.active) unlockPro()
 *
 * Identity works the way it must for cross-platform entitlements: a subscriber
 * who has not signed in yet gets an anonymous id, generated here and kept in
 * local storage, so a purchase can happen before sign-up. `logIn()` then merges
 * that anonymous subject into the real one.
 */

import { asBool, asDate, asNumber, asString, pick } from './decode.js'
import type {
  CustomerInfo,
  EntitlementInfo,
  OfferingsResponse,
  Product,
  ResolvedPackage,
  Subscription,
  SubscriptionStatus,
} from './types.js'

export type FetchLike = (input: string, init?: any) => Promise<any>

/** Where the anonymous id is kept between visits. */
export interface Storage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

const ANON_KEY = 'cashier.anonymousId'

/**
 * Cloud's API origin.
 *
 * Deliberately a constant, not an option. The public key is scoped to this
 * host, so a page that could repoint the SDK could hand that key — and every
 * subscriber read made with it — to whoever owns the other host. Nothing a
 * developer legitimately needs requires moving it: tests and proxies inject
 * `fetch` instead, which is narrower and explicit.
 */
const API_ORIGIN = 'https://nimbusgo.space'

export interface CashierCloudOptions {
  /** The public SDK key for this app, e.g. `cshr_web_…`. Safe to ship in a page. */
  apiKey: string
  /**
   * Your own user id. Omit it and an anonymous id is generated and persisted,
   * so a purchase can happen before the person signs up.
   */
  appUserId?: string
  headers?: Record<string, string>
  /** Injected fetch (tests, SSR, a custom agent). */
  fetch?: FetchLike
  /** Injected storage. Defaults to `localStorage`, or memory where absent. */
  storage?: Storage
}

/**
 * What went wrong, as a stable string rather than an HTTP status.
 *
 * Branch on `code`, not on `status` or the message: statuses get reused and
 * messages get reworded, and neither is a contract.
 */
export const CashierErrorCode = {
  Unknown: 'unknown',
  /** The request never reached Cloud — offline, DNS, CORS, a dropped connection. */
  Network: 'network_error',
  /** The key is missing, malformed, revoked, or a secret key used in a browser. */
  InvalidApiKey: 'invalid_api_key',
  /** The app user id is unusable — empty, or a reserved value. */
  InvalidAppUserId: 'invalid_app_user_id',
  /** No offering is configured, or the named one does not exist. */
  OfferingNotFound: 'offering_not_found',
  /** The product or package asked for is not purchasable right now. */
  ProductNotAvailable: 'product_not_available',
  /** The subscriber has never been seen. */
  SubscriberNotFound: 'subscriber_not_found',
  /** The account is over its plan, or the entitlement is not paid for. */
  PaymentRequired: 'payment_required',
  /** The customer closed or abandoned checkout. Not an error to report. */
  UserCancelled: 'user_cancelled',
  /** Checkout could not be started. */
  CheckoutFailed: 'checkout_failed',
  RateLimited: 'rate_limited',
  ServerError: 'server_error',
} as const

export type CashierErrorCode = (typeof CashierErrorCode)[keyof typeof CashierErrorCode]

/** An error from the Cashier Cloud API. */
export class CashierCloudError extends Error {
  constructor(
    message: string,
    readonly code: CashierErrorCode,
    /** The HTTP status, or 0 when the request never got a response. */
    readonly status: number,
    readonly body?: unknown,
  ) {
    super(message)
    this.name = 'CashierCloudError'
  }

  /** True when Cloud refused for want of an entitlement. */
  get paymentRequired(): boolean {
    return this.code === CashierErrorCode.PaymentRequired
  }

  /** True when the customer simply walked away — usually not worth surfacing. */
  get userCancelled(): boolean {
    return this.code === CashierErrorCode.UserCancelled
  }
}

/** Maps a response onto a stable code, preferring one Cloud sent explicitly. */
function errorCodeFor(status: number, body: unknown): CashierErrorCode {
  const explicit = (body as any)?.code
  if (typeof explicit === 'string' && (Object.values(CashierErrorCode) as string[]).includes(explicit)) {
    return explicit as CashierErrorCode
  }
  switch (status) {
    case 0:
      return CashierErrorCode.Network
    case 401:
    case 403:
      return CashierErrorCode.InvalidApiKey
    case 402:
      return CashierErrorCode.PaymentRequired
    case 404:
      return CashierErrorCode.SubscriberNotFound
    case 429:
      return CashierErrorCode.RateLimited
    default:
      return status >= 500 ? CashierErrorCode.ServerError : CashierErrorCode.Unknown
  }
}

export type CustomerInfoListener = (info: CustomerInfo) => void

export class CashierCloud {
  private readonly opts: CashierCloudOptions
  private readonly doFetch: FetchLike
  private readonly storage: Storage
  private subject: string
  private cached: CustomerInfo | null = null
  private inflight: Promise<CustomerInfo> | null = null
  private readonly listeners = new Set<CustomerInfoListener>()

  private static shared: CashierCloud | null = null

  /**
   * Configures the SDK once for the page and returns the shared instance.
   *
   * Identity and the CustomerInfo cache are per-instance, so two instances
   * would disagree about who is signed in. Configure once and reach for
   * {@link getSharedInstance} elsewhere.
   */
  static configure(options: CashierCloudOptions): CashierCloud {
    CashierCloud.shared = new CashierCloud(options)
    return CashierCloud.shared
  }

  /** The instance {@link configure} created. */
  static getSharedInstance(): CashierCloud {
    if (!CashierCloud.shared) {
      throw new Error('cashier-cloud: call CashierCloud.configure() before getSharedInstance()')
    }
    return CashierCloud.shared
  }

  /**
   * Mints an anonymous app user id without configuring the SDK — for an app
   * that would rather persist the id itself than let the SDK use local storage.
   */
  static generateAnonymousAppUserId(): string {
    return `$anon:${randomId()}`
  }

  constructor(options: CashierCloudOptions) {
    if (!options?.apiKey) throw new Error('cashier-cloud: an apiKey is required')
    this.opts = options

    const injected = options.fetch ?? (globalThis as any).fetch
    if (!injected) throw new Error('cashier-cloud: no fetch available — pass options.fetch')
    this.doFetch = injected.bind(globalThis)

    this.storage = options.storage ?? defaultStorage()
    this.subject = options.appUserId || this.anonymousId()
  }

  /** The subject every read and purchase is attributed to. */
  get appUserId(): string {
    return this.subject
  }

  /** Whether the current subject is an anonymous one this SDK generated. */
  get isAnonymous(): boolean {
    return this.subject.startsWith('$anon:')
  }

  // ── Paywall ─────────────────────────────────────────────────────

  /**
   * The current offering, with each package's product resolved.
   *
   * Pass a `currency` to price it in something other than the subscriber's
   * default — a paywall shown to a visitor whose country you already know
   * should not quote them dollars first and correct itself later.
   */
  async offerings(options: { currency?: string } = {}): Promise<OfferingsResponse> {
    const query = options.currency ? `?currency=${encodeURIComponent(options.currency)}` : ''
    return decodeOfferings(await this.request('GET', `/v1/offerings${query}`))
  }

  /** The current offering's packages, for a paywall that only renders a list. */
  async packages(options: { currency?: string } = {}): Promise<ResolvedPackage[]> {
    return (await this.offerings(options)).packages
  }

  // ── Subscriber state ────────────────────────────────────────────

  /**
   * The subscriber's aggregate state. Cached after the first call; pass
   * `{ force: true }` to refetch — for instance after returning from checkout.
   */
  async customerInfo(options: { force?: boolean } = {}): Promise<CustomerInfo> {
    if (!options.force && this.cached) return this.cached
    if (!options.force && this.inflight) return this.inflight

    const promise = this.request('GET', `/v1/subscribers/${encodeURIComponent(this.subject)}`)
      .then((body) => {
        const info = decodeCustomerInfo(body)
        this.cached = info
        this.emit(info)
        return info
      })
      .finally(() => {
        this.inflight = null
      })

    this.inflight = promise
    return promise
  }

  /** The last fetched CustomerInfo, without a request. Null before the first fetch. */
  get current(): CustomerInfo | null {
    return this.cached
  }

  /**
   * Whether the last fetched CustomerInfo holds an active entitlement.
   * Synchronous, so only as fresh as the last fetch — gate anything that
   * matters on your server reading Cloud, not on this.
   */
  isEntitled(entitlementId: string): boolean {
    const e = this.cached?.entitlements[entitlementId]
    return !!e && e.active
  }

  /** Drops the cached CustomerInfo so the next read refetches. */
  invalidate(): void {
    this.cached = null
  }

  /** Subscribes to CustomerInfo changes. Returns an unsubscribe function. */
  onCustomerInfoUpdate(listener: CustomerInfoListener): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  // ── Identity ────────────────────────────────────────────────────

  /**
   * Attaches this device's subscriber to your own user id, merging anything
   * bought anonymously into it. Idempotent — calling it twice for the same
   * user is a no-op on Cloud's side.
   */
  async logIn(appUserId: string): Promise<CustomerInfo> {
    if (!appUserId) throw new Error('cashier-cloud: logIn needs an app user id')
    const previous = this.subject
    if (previous === appUserId) return this.customerInfo()

    const body = await this.request('POST', `/v1/subscribers/${encodeURIComponent(previous)}/alias`, {
      newAppUserId: appUserId,
    })
    this.subject = appUserId
    // The anonymous id has been merged away; keeping it would resurrect an
    // empty subscriber on the next visit.
    if (previous.startsWith('$anon:')) this.storage.removeItem(ANON_KEY)

    const info = decodeCustomerInfo(body)
    this.cached = info
    this.emit(info)
    return info
  }

  /**
   * Detaches from the identified user and starts a fresh anonymous subscriber.
   * Entitlements stay with the account they were bought under.
   */
  async logOut(): Promise<CustomerInfo> {
    this.storage.removeItem(ANON_KEY)
    this.subject = this.anonymousId()
    this.cached = null
    return this.customerInfo({ force: true })
  }

  // ── Purchasing ──────────────────────────────────────────────────

  /**
   * Starts a web checkout for a package and sends the browser to it.
   *
   * Web purchases settle through Stripe, so this navigates away and the
   * promise never resolves — treat the call as the end of the page's life, and
   * refetch CustomerInfo on the success URL.
   */
  async purchase(
    target: ResolvedPackage | Product | string,
    options: {
      successUrl?: string
      cancelUrl?: string
      /** Set false to receive the URL instead of navigating to it. */
      redirect?: boolean
      /** Pre-fills checkout, skipping the email step. */
      customerEmail?: string
      /** BCP-47 tag for the checkout's language, e.g. "hi-IN". */
      locale?: string
      /** Carried through to your webhook on the resulting subscriber event. */
      metadata?: Record<string, string>
    } = {},
  ): Promise<{ url: string }> {
    const packageId = typeof target === 'string' ? undefined : 'product' in target ? target.id : undefined
    const productId = typeof target === 'string' ? target : 'product' in target ? target.product.id : target.id

    const body = await this.request('POST', '/v1/checkout', {
      packageId,
      productId,
      successUrl: options.successUrl ?? currentUrl(),
      cancelUrl: options.cancelUrl ?? currentUrl(),
      customerEmail: options.customerEmail,
      locale: options.locale,
      metadata: options.metadata,
    })
    const url = asString(pick(body, 'url', 'checkout_url'))
    if (!url) {
      throw new CashierCloudError(
        'cashier-cloud: checkout returned no URL',
        CashierErrorCode.CheckoutFailed,
        502,
        body,
      )
    }

    if (options.redirect !== false) {
      const location = (globalThis as any)?.location
      if (location?.assign) location.assign(url)
    }
    return { url }
  }

  // ── Internals ───────────────────────────────────────────────────

  /** Reads the persisted anonymous id, minting and storing one on first use. */
  private anonymousId(): string {
    const existing = this.storage.getItem(ANON_KEY)
    if (existing) return existing
    const id = `$anon:${randomId()}`
    this.storage.setItem(ANON_KEY, id)
    return id
  }

  private async request(method: string, path: string, body?: unknown): Promise<any> {
    const headers: Record<string, string> = {
      Accept: 'application/json',
      Authorization: `Bearer ${this.opts.apiKey}`,
      ...(this.opts.headers ?? {}),
    }
    if (body !== undefined) headers['Content-Type'] = 'application/json'

    let res: any
    try {
      res = await this.doFetch(API_ORIGIN + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      })
    } catch (cause) {
      // Offline, DNS, CORS, a dropped connection — a raw TypeError here would
      // be indistinguishable from a bug in the caller's own code.
      throw new CashierCloudError(
        `cashier-cloud: ${method} ${path} could not reach Cloud`,
        CashierErrorCode.Network,
        0,
        cause,
      )
    }

    const text = await res.text()
    let decoded: unknown = undefined
    if (text) {
      try {
        decoded = JSON.parse(text)
      } catch {
        decoded = text
      }
    }
    if (!res.ok) {
      const message = errorMessage(decoded) ?? `cashier-cloud: ${method} ${path} failed with ${res.status}`
      throw new CashierCloudError(message, errorCodeFor(res.status, decoded), res.status, decoded)
    }
    return decoded
  }

  private emit(info: CustomerInfo): void {
    for (const listener of this.listeners) {
      try {
        listener(info)
      } catch {
        // A listener must not break the fetch that produced the update.
      }
    }
  }
}

// ── Helpers ───────────────────────────────────────────────────────

function errorMessage(body: unknown): string | undefined {
  if (typeof body === 'string' && body) return body
  if (body && typeof body === 'object') {
    const err = (body as any).error ?? (body as any).message
    if (typeof err === 'string') return `cashier-cloud: ${err}`
  }
  return undefined
}

function currentUrl(): string | undefined {
  return (globalThis as any)?.location?.href
}

function randomId(): string {
  const crypto = (globalThis as any).crypto
  if (crypto?.randomUUID) return crypto.randomUUID()
  if (crypto?.getRandomValues) {
    const bytes = crypto.getRandomValues(new Uint8Array(16))
    return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('')
  }
  return `${Date.now().toString(16)}${Math.random().toString(16).slice(2)}`
}

/** localStorage where it exists, memory where it does not (SSR, private mode). */
function defaultStorage(): Storage {
  try {
    const ls = (globalThis as any).localStorage
    if (ls) {
      // Private mode can expose the API and throw on write; find out now.
      ls.setItem('cashier.probe', '1')
      ls.removeItem('cashier.probe')
      return ls
    }
  } catch {
    // fall through to memory
  }
  const map = new Map<string, string>()
  return {
    getItem: (k) => map.get(k) ?? null,
    setItem: (k, v) => void map.set(k, v),
    removeItem: (k) => void map.delete(k),
  }
}

// ── Decoders ──────────────────────────────────────────────────────

export function decodeProduct(raw: any): Product {
  return {
    id: asString(pick(raw, 'id')),
    name: asString(pick(raw, 'name')) || undefined,
    amount: asNumber(pick(raw, 'amount')),
    currency: asString(pick(raw, 'currency')) || undefined,
    periodMonths: asNumber(pick(raw, 'period_months')),
    trialDays: asNumber(pick(raw, 'trial_days')),
    entitlements: (pick<string[]>(raw, 'entitlements') ?? []).map((e) => asString(e)),
  }
}

export function decodeOfferings(raw: any): OfferingsResponse {
  const packages = (pick<any[]>(raw, 'packages') ?? []).map((p) => ({
    id: asString(pick(p, 'id')),
    product: decodeProduct(pick(p, 'product') ?? {}),
  }))
  return {
    currentOffering: asString(pick(raw, 'current_offering', 'id')),
    metadata: pick<Record<string, string>>(raw, 'metadata'),
    packages,
    // Paywall code wants "the monthly one", not packages[0]. Derived from the
    // product's billing period rather than the package id, so a package called
    // "starter" still resolves correctly.
    monthly: packages.find((p) => p.product.periodMonths === 1),
    annual: packages.find((p) => p.product.periodMonths === 12),
    lifetime: packages.find((p) => (p.product.periodMonths ?? 0) === 0 && (p.product.amount ?? 0) > 0),
  }
}

export function decodeEntitlementInfo(raw: any, fallbackId = ''): EntitlementInfo {
  return {
    id: asString(pick(raw, 'id'), fallbackId),
    active: asBool(pick(raw, 'active')),
    willRenew: asBool(pick(raw, 'will_renew')),
    periodType: pick(raw, 'period_type'),
    productId: asString(pick(raw, 'product_id')) || undefined,
    source: pick(raw, 'source'),
    expiresAt: asDate(pick(raw, 'expires_at')),
  }
}

export function decodeSubscription(raw: any): Subscription {
  return {
    store: asString(pick(raw, 'store', 'platform')),
    id: asString(pick(raw, 'id')),
    status: asString(pick(raw, 'status'), 'unknown') as SubscriptionStatus,
    productId: asString(pick(raw, 'product_id')) || undefined,
    subject: asString(pick(raw, 'subject')) || undefined,
    currentPeriodEnd: asDate(pick(raw, 'current_period_end')),
    trialEnd: asDate(pick(raw, 'trial_end')),
    canceledAt: asDate(pick(raw, 'canceled_at')),
    endedAt: asDate(pick(raw, 'ended_at')),
  }
}

export function decodeCustomerInfo(raw: any): CustomerInfo {
  const entitlements: Record<string, EntitlementInfo> = {}
  const rawEntitlements = pick<Record<string, any>>(raw, 'entitlements') ?? {}
  for (const [id, value] of Object.entries(rawEntitlements)) {
    entitlements[id] = decodeEntitlementInfo(value, id)
  }
  return {
    subject: asString(pick(raw, 'subject', 'app_user_id')),
    requestedAt: asDate(pick(raw, 'requested_at')) ?? new Date(),
    entitlements,
    activeEntitlementIds: (pick<string[]>(raw, 'active_entitlement_ids') ?? []).map((s) => asString(s)),
    activeProductIds: (pick<string[]>(raw, 'active_product_ids') ?? []).map((s) => asString(s)),
    activeSubscriptions: (pick<any[]>(raw, 'active_subscriptions') ?? []).map(decodeSubscription),
    latestExpiresAt: asDate(pick(raw, 'latest_expires_at')),
  }
}
