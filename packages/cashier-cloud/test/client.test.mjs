import assert from 'node:assert/strict'
import test from 'node:test'

import { CashierCloud, CashierCloudError, CashierErrorCode, hasEntitlement } from '../dist/index.js'

/** A fetch double that records calls and replays canned JSON. */
function fakeFetch(routes) {
  const calls = []
  const doFetch = async (url, init = {}) => {
    const parsed = new URL(url)
    const path = decodeURIComponent(parsed.pathname)
    const key = `${init.method ?? 'GET'} ${path}${parsed.search}`
    calls.push({ key, headers: init.headers, body: init.body ? JSON.parse(init.body) : undefined })
    const handler = routes[key] ?? routes[key.split('?')[0]]
    if (!handler) return new Response(JSON.stringify({ error: 'not_found' }), { status: 404 })
    const { status = 200, body } = typeof handler === 'function' ? handler() : handler
    return new Response(JSON.stringify(body), { status })
  }
  doFetch.calls = calls
  return doFetch
}

/** Memory storage, so anonymous-id behaviour is observable in tests. */
function memoryStorage(seed = {}) {
  const map = new Map(Object.entries(seed))
  return {
    getItem: (k) => map.get(k) ?? null,
    setItem: (k, v) => void map.set(k, v),
    removeItem: (k) => void map.delete(k),
    dump: () => Object.fromEntries(map),
  }
}

const SUBSCRIBER = (subject, entitlements = {}) => ({
  subject,
  requested_at: new Date().toISOString(),
  entitlements,
  active_entitlement_ids: Object.keys(entitlements).filter((k) => entitlements[k].active),
  active_product_ids: [],
})

// ── Configuration ─────────────────────────────────────────────────

test('an api key is required', () => {
  assert.throws(() => new CashierCloud({}), /apiKey is required/)
})

test('the public key travels as a bearer token on every call', async () => {
  const fetch = fakeFetch({ 'GET /v1/offerings': { body: { current_offering: 'default', packages: [] } } })
  const cashier = new CashierCloud({ apiKey: 'cshr_web_abc', fetch, storage: memoryStorage() })

  await cashier.offerings()
  assert.equal(fetch.calls[0].headers.Authorization, 'Bearer cshr_web_abc')
})

// ── Identity ──────────────────────────────────────────────────────

test('a subscriber with no user id gets a persisted anonymous one', () => {
  const storage = memoryStorage()
  const first = new CashierCloud({ apiKey: 'k', fetch: fakeFetch({}), storage })

  assert.equal(first.isAnonymous, true)
  assert.match(first.appUserId, /^\$anon:/)

  // A second visit reuses it, or a returning buyer would lose their purchase.
  const second = new CashierCloud({ apiKey: 'k', fetch: fakeFetch({}), storage })
  assert.equal(second.appUserId, first.appUserId)
})

test('an explicit app user id wins over the anonymous one', () => {
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch: fakeFetch({}), storage: memoryStorage() })
  assert.equal(cashier.appUserId, 'user_7')
  assert.equal(cashier.isAnonymous, false)
})

test('logIn aliases the anonymous subject and forgets it afterwards', async () => {
  const storage = memoryStorage()
  const fetch = fakeFetch({
    'POST /v1/subscribers/$anon:abc/alias': { body: SUBSCRIBER('user_7', { premium: { id: 'premium', active: true } }) },
  })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage({ 'cashier.anonymousId': '$anon:abc' }) })

  const info = await cashier.logIn('user_7')
  assert.equal(fetch.calls[0].body.newAppUserId, 'user_7')
  assert.equal(cashier.appUserId, 'user_7')
  assert.equal(cashier.isAnonymous, false)
  assert.equal(hasEntitlement(info, 'premium'), true)
})

test('logging in as the current user is a no-op, not a second alias', async () => {
  const fetch = fakeFetch({ 'GET /v1/subscribers/user_7': { body: SUBSCRIBER('user_7') } })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  await cashier.logIn('user_7')
  assert.deepEqual(fetch.calls.map((c) => c.key), ['GET /v1/subscribers/user_7'])
})

test('logIn refuses an empty id rather than aliasing to nothing', async () => {
  const cashier = new CashierCloud({ apiKey: 'k', fetch: fakeFetch({}), storage: memoryStorage() })
  await assert.rejects(() => cashier.logIn(''), /needs an app user id/)
})

test('logOut starts a fresh anonymous subscriber', async () => {
  const storage = memoryStorage()
  const fetch = fakeFetch({})
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage })

  // The subscriber endpoint 404s for the new anon id; identity still rotated.
  await cashier.logOut().catch(() => undefined)
  assert.equal(cashier.isAnonymous, true)
  assert.notEqual(cashier.appUserId, 'user_7')
})

// ── Reading state ─────────────────────────────────────────────────

test('customerInfo decodes, caches and notifies', async () => {
  const expiry = new Date(Date.now() + 86_400_000)
  const fetch = fakeFetch({
    'GET /v1/subscribers/user_7': {
      body: SUBSCRIBER('user_7', {
        premium: {
          id: 'premium', active: true, will_renew: false, period_type: 'grace',
          product_id: 'pro_monthly', expires_at: expiry.toISOString(),
        },
      }),
    },
  })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  const seen = []
  const off = cashier.onCustomerInfoUpdate((i) => seen.push(i))

  const info = await cashier.customerInfo()
  assert.equal(info.entitlements.premium.willRenew, false)
  assert.equal(info.entitlements.premium.periodType, 'grace')
  assert.equal(info.entitlements.premium.expiresAt.getTime(), expiry.getTime())
  assert.equal(cashier.isEntitled('premium'), true)
  assert.equal(cashier.isEntitled('missing'), false)

  await cashier.customerInfo()
  assert.equal(fetch.calls.length, 1, 'the second read is served from the cache')

  await cashier.customerInfo({ force: true })
  assert.equal(fetch.calls.length, 2)
  assert.equal(seen.length, 2)

  off()
  await cashier.customerInfo({ force: true })
  assert.equal(seen.length, 2, 'an unsubscribed listener stops hearing updates')
})

test('a never-expiring entitlement decodes as null, not year one', async () => {
  const fetch = fakeFetch({
    'GET /v1/subscribers/user_7': {
      body: SUBSCRIBER('user_7', { premium: { id: 'premium', active: true, expires_at: '0001-01-01T00:00:00Z' } }),
    },
  })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  const info = await cashier.customerInfo()
  assert.equal(info.entitlements.premium.expiresAt, null)
})

test('a 402 surfaces as an error that says so', async () => {
  const fetch = fakeFetch({
    'GET /v1/offerings': { status: 402, body: { error: 'payment_required' } },
  })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  await assert.rejects(
    () => cashier.offerings(),
    (err) => {
      assert.ok(err instanceof CashierCloudError)
      assert.equal(err.paymentRequired, true)
      return true
    },
  )
})

// ── Paywall and checkout ──────────────────────────────────────────

test('offerings decode with their products resolved', async () => {
  const fetch = fakeFetch({
    'GET /v1/offerings': {
      body: {
        current_offering: 'default',
        metadata: { headline: 'Go Pro' },
        packages: [
          { id: 'monthly', product: { id: 'pro_monthly', name: 'Pro', amount: 149900, currency: 'INR', period_months: 1, entitlements: ['premium'] } },
        ],
      },
    },
  })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  const offerings = await cashier.offerings()
  assert.equal(offerings.currentOffering, 'default')
  assert.equal(offerings.packages[0].product.periodMonths, 1)
  assert.deepEqual(offerings.packages[0].product.entitlements, ['premium'])
})

test('purchase asks Cloud for a checkout url and returns it without navigating', async () => {
  const fetch = fakeFetch({
    'POST /v1/checkout': { body: { url: 'https://checkout.stripe.com/c/session_1' } },
  })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  const { url } = await cashier.purchase(
    { id: 'monthly', product: { id: 'pro_monthly' } },
    { successUrl: 'https://app/ok', cancelUrl: 'https://app/no', redirect: false },
  )

  assert.equal(url, 'https://checkout.stripe.com/c/session_1')
  assert.deepEqual(fetch.calls[0].body, {
    packageId: 'monthly',
    productId: 'pro_monthly',
    successUrl: 'https://app/ok',
    cancelUrl: 'https://app/no',
  })
})

test('a checkout with no url is a server error, not a silent no-op', async () => {
  const fetch = fakeFetch({ 'POST /v1/checkout': { body: {} } })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  await assert.rejects(() => cashier.purchase('pro_monthly', { redirect: false }), /returned no URL/)
})

// ── Error codes ───────────────────────────────────────────────────

test('errors carry a stable code, not just an HTTP status', async () => {
  const cases = [
    [401, 'invalid_api_key'],
    [402, 'payment_required'],
    [404, 'subscriber_not_found'],
    [429, 'rate_limited'],
    [503, 'server_error'],
    [418, 'unknown'],
  ]

  for (const [status, code] of cases) {
    const fetch = fakeFetch({ 'GET /v1/offerings': { status, body: {} } })
    const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })
    await assert.rejects(
      () => cashier.offerings(),
      (err) => {
        assert.equal(err.code, code, `status ${status}`)
        assert.equal(err.status, status)
        return true
      },
    )
  }
})

test('a code Cloud sends explicitly wins over the status mapping', async () => {
  const fetch = fakeFetch({
    'GET /v1/offerings': { status: 404, body: { code: 'offering_not_found', error: 'no offering configured' } },
  })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  await assert.rejects(cashier.offerings(), (err) => {
    assert.equal(err.code, CashierErrorCode.OfferingNotFound)
    return true
  })
})

test('a request that never reaches Cloud is a typed network error, not a TypeError', async () => {
  const fetch = async () => { throw new TypeError('Failed to fetch') }
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  await assert.rejects(cashier.offerings(), (err) => {
    assert.ok(err instanceof CashierCloudError)
    assert.equal(err.code, CashierErrorCode.Network)
    assert.equal(err.status, 0, 'no response means no status')
    return true
  })
})

test('paymentRequired and userCancelled read the code, not the status', async () => {
  const fetch = fakeFetch({ 'GET /v1/offerings': { status: 402, body: {} } })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  await assert.rejects(cashier.offerings(), (err) => {
    assert.equal(err.paymentRequired, true)
    assert.equal(err.userCancelled, false)
    return true
  })
})

// ── Offerings convenience ─────────────────────────────────────────

test('packages are addressable by billing period, not by index', async () => {
  const fetch = fakeFetch({
    'GET /v1/offerings': {
      body: {
        current_offering: 'default',
        packages: [
          { id: 'starter', product: { id: 'pro_monthly', amount: 149900, period_months: 1 } },
          { id: 'best_value', product: { id: 'pro_annual', amount: 1499900, period_months: 12 } },
          { id: 'forever', product: { id: 'pro_lifetime', amount: 4999900, period_months: 0 } },
        ],
      },
    },
  })
  const offerings = await new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() }).offerings()

  // The package ids are arbitrary; the billing period is what a paywall means.
  assert.equal(offerings.monthly.product.id, 'pro_monthly')
  assert.equal(offerings.annual.product.id, 'pro_annual')
  assert.equal(offerings.lifetime.product.id, 'pro_lifetime')
})

test('a currency is passed through to Cloud', async () => {
  const fetch = fakeFetch({ 'GET /v1/offerings': { body: { current_offering: 'd', packages: [] } } })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage() })

  await cashier.offerings({ currency: 'EUR' })
  assert.match(fetch.calls[0].key, /currency=EUR/)
})

// ── Purchase params ───────────────────────────────────────────────

test('checkout carries the email, locale and metadata it was given', async () => {
  const fetch = fakeFetch({ 'POST /v1/checkout': { body: { url: 'https://checkout.example/x' } } })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u1', fetch, storage: memoryStorage() })

  await cashier.purchase('pro_monthly', {
    redirect: false,
    customerEmail: 'a@example.com',
    locale: 'hi-IN',
    metadata: { campaign: 'diwali' },
  })

  assert.equal(fetch.calls[0].body.customerEmail, 'a@example.com')
  assert.equal(fetch.calls[0].body.locale, 'hi-IN')
  assert.deepEqual(fetch.calls[0].body.metadata, { campaign: 'diwali' })
})

// ── Singleton ─────────────────────────────────────────────────────

test('configure sets up a shared instance, and reaching for it early is an error', () => {
  assert.throws(() => CashierCloud.getSharedInstance(), /call CashierCloud.configure\(\)/)

  const configured = CashierCloud.configure({ apiKey: 'k', fetch: fakeFetch({}), storage: memoryStorage() })
  assert.equal(CashierCloud.getSharedInstance(), configured)
})

test('an anonymous id can be minted without configuring anything', () => {
  const id = CashierCloud.generateAnonymousAppUserId()
  assert.match(id, /^\$anon:/)
  assert.notEqual(id, CashierCloud.generateAnonymousAppUserId())
})
