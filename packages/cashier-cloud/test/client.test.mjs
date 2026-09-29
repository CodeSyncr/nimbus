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
  const fetch = fakeFetch({ 'GET /api/cashier/v1/offerings': { body: { current_offering: 'default', packages: [] } } })
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
    'POST /api/cashier/v1/subscribers/$anon:abc/alias': { body: SUBSCRIBER('user_7', { premium: { id: 'premium', active: true } }) },
  })
  const cashier = new CashierCloud({ apiKey: 'k', fetch, storage: memoryStorage({ 'cashier.anonymousId': '$anon:abc' }) })

  const info = await cashier.logIn('user_7')
  assert.equal(fetch.calls[0].body.newAppUserId, 'user_7')
  assert.equal(cashier.appUserId, 'user_7')
  assert.equal(cashier.isAnonymous, false)
  assert.equal(hasEntitlement(info, 'premium'), true)
})

test('logging in as the current user is a no-op, not a second alias', async () => {
  const fetch = fakeFetch({ 'GET /api/cashier/v1/subscribers/user_7': { body: SUBSCRIBER('user_7') } })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  await cashier.logIn('user_7')
  assert.deepEqual(fetch.calls.map((c) => c.key), ['GET /api/cashier/v1/subscribers/user_7'])
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
    'GET /api/cashier/v1/subscribers/user_7': {
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
    'GET /api/cashier/v1/subscribers/user_7': {
      body: SUBSCRIBER('user_7', { premium: { id: 'premium', active: true, expires_at: '0001-01-01T00:00:00Z' } }),
    },
  })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  const info = await cashier.customerInfo()
  assert.equal(info.entitlements.premium.expiresAt, null)
})

test('a 402 surfaces as an error that says so', async () => {
  const fetch = fakeFetch({
    'GET /api/cashier/v1/offerings': { status: 402, body: { error: 'payment_required' } },
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
    'GET /api/cashier/v1/offerings': {
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

test('purchase opens a hosted checkout for the package and returns its url', async () => {
  const fetch = fakeFetch({
    'POST /api/cashier/v1/checkout': { body: { url: 'https://checkout.stripe.com/c/pay/cs_1' } },
  })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user_7', fetch, storage: memoryStorage() })

  const out = await cashier.purchase(
    { id: '$monthly', product: { id: 'pro_monthly' } },
    { successUrl: 'https://shop.example/thanks', cancelUrl: 'https://shop.example/pricing', offeringId: 'default' },
  )
  assert.deepEqual(out, { url: 'https://checkout.stripe.com/c/pay/cs_1' })
  assert.equal(fetch.calls.length, 1)
  assert.equal(fetch.calls[0].key, 'POST /api/cashier/v1/checkout')
  assert.deepEqual(fetch.calls[0].body, {
    app_user_id: 'user_7',
    product_id: 'pro_monthly',
    package_id: '$monthly',
    offering_id: 'default',
    success_url: 'https://shop.example/thanks',
    cancel_url: 'https://shop.example/pricing',
  })
})

test('purchase takes a product or a product id too', async () => {
  const fetch = fakeFetch({ 'POST /api/cashier/v1/checkout': { body: { url: 'https://checkout.stripe.com/x' } } })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch, storage: memoryStorage() })
  const urls = { successUrl: 'https://a.example/ok', cancelUrl: 'https://a.example/no' }

  await cashier.purchase({ id: 'lifetime', amount: 9999 }, urls)
  await cashier.purchase('pro_annual', urls)
  assert.equal(fetch.calls[0].body.product_id, 'lifetime')
  assert.equal(fetch.calls[0].body.package_id, undefined)
  assert.equal(fetch.calls[1].body.product_id, 'pro_annual')
})

test('in a browser, purchase navigates to checkout and defaults the return urls to this page', async () => {
  const assigned = []
  const saved = globalThis.location
  globalThis.location = { href: 'https://shop.example/pricing?plan=pro', assign: (u) => assigned.push(u) }
  try {
    const fetch = fakeFetch({ 'POST /api/cashier/v1/checkout': { body: { url: 'https://checkout.stripe.com/c/pay/cs_2' } } })
    const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch, storage: memoryStorage() })

    await cashier.purchase('pro_monthly')
    assert.deepEqual(assigned, ['https://checkout.stripe.com/c/pay/cs_2'])
    assert.equal(fetch.calls[0].body.success_url, 'https://shop.example/pricing?plan=pro')
    assert.equal(fetch.calls[0].body.cancel_url, 'https://shop.example/pricing?plan=pro')

    await cashier.purchase('pro_monthly', { redirect: false })
    assert.equal(assigned.length, 1, 'redirect: false only returns the url')
  } finally {
    globalThis.location = saved
  }
})

test('outside a browser, purchase needs the return urls', async () => {
  const fetch = fakeFetch({})
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch, storage: memoryStorage() })
  await assert.rejects(() => cashier.purchase('pro_monthly'), (err) => err.code === CashierErrorCode.CheckoutFailed)
  await assert.rejects(() => cashier.purchase(''), (err) => err.code === CashierErrorCode.ProductNotAvailable)
  assert.equal(fetch.calls.length, 0)
})

test('an app without web payments connected rejects with checkout_not_configured', async () => {
  const fetch = fakeFetch({
    'POST /api/cashier/v1/checkout': {
      status: 409,
      body: { error: 'checkout_not_configured', message: 'This app has no web payment provider connected.' },
    },
  })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch, storage: memoryStorage() })

  await assert.rejects(
    () => cashier.purchase('pro_monthly', { successUrl: 'https://a.example', cancelUrl: 'https://a.example' }),
    (err) => {
      assert.ok(err instanceof CashierCloudError)
      assert.equal(err.code, CashierErrorCode.CheckoutNotConfigured)
      assert.equal(err.status, 409)
      assert.match(err.message, /no web payment provider/)
      return true
    },
  )
  // Older servers' answer keeps its own code.
  const old = fakeFetch({ 'POST /api/cashier/v1/checkout': { status: 501, body: { error: 'checkout_unavailable', message: 'no' } } })
  const legacy = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch: old, storage: memoryStorage() })
  await assert.rejects(
    () => legacy.purchase('pro_monthly', { successUrl: 'https://a.example', cancelUrl: 'https://a.example' }),
    (err) => err.code === CashierErrorCode.CheckoutUnavailable,
  )
  // An unknown product maps onto product_not_available.
  const unknown = fakeFetch({ 'POST /api/cashier/v1/checkout': { status: 422, body: { error: 'unknown_product', message: 'unknown product' } } })
  const c3 = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch: unknown, storage: memoryStorage() })
  await assert.rejects(
    () => c3.purchase('nope', { successUrl: 'https://a.example', cancelUrl: 'https://a.example' }),
    (err) => err.code === CashierErrorCode.ProductNotAvailable,
  )
})

test('presentPaywall without onPurchase sends the chosen package to hosted checkout', async () => {
  const { Window } = await import('happy-dom')
  const win = new Window()
  const assigned = []
  const saved = { document: globalThis.document, location: globalThis.location }
  globalThis.document = win.document
  globalThis.location = { href: 'https://shop.example/app', assign: (u) => assigned.push(u) }
  try {
    const fetch = fakeFetch({
      'GET /api/cashier/v1/offerings': {
        body: {
          current_offering: 'default',
          app_name: 'Acme',
          packages: [{ id: '$monthly', product: { id: 'pro_monthly', name: 'Pro', amount: 499, currency: 'USD', period_months: 1 } }],
          paywall: {
            version: 1,
            theme: { background: '#FFFFFF', surface: '#F4F4F6', text: '#111114', muted: '#6B6B76', accent: '#F04E28', accent_text: '#FFFFFF', radius: 16, font: 'system' },
            blocks: [
              { id: 'title', type: 'title', text: 'Go Pro' },
              { id: 'packages', type: 'packages', layout: 'list' },
              { id: 'cta', type: 'cta', text: 'Continue' },
            ],
          },
        },
      },
      'POST /api/cashier/v1/checkout': { body: { url: 'https://checkout.stripe.com/c/pay/cs_pw' } },
    })
    const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u', fetch, storage: memoryStorage() })
    void cashier.presentPaywall()
    for (let i = 0; i < 20 && !win.document.body.querySelector('button'); i++) await new Promise((r) => setTimeout(r, 5))
    const cta = [...win.document.body.querySelectorAll('button')].find((b) => b.textContent === 'Continue')
    assert.ok(cta, 'the paywall rendered its purchase button')
    cta.click()
    for (let i = 0; i < 20 && assigned.length === 0; i++) await new Promise((r) => setTimeout(r, 5))

    assert.deepEqual(assigned, ['https://checkout.stripe.com/c/pay/cs_pw'])
    const checkout = fetch.calls.find((c) => c.key === 'POST /api/cashier/v1/checkout')
    assert.deepEqual(checkout.body, {
      app_user_id: 'u',
      product_id: 'pro_monthly',
      package_id: '$monthly',
      offering_id: 'default',
      success_url: 'https://shop.example/app',
      cancel_url: 'https://shop.example/app',
    })
  } finally {
    globalThis.document = saved.document
    globalThis.location = saved.location
    await win.happyDOM.close()
  }
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
    const fetch = fakeFetch({ 'GET /api/cashier/v1/offerings': { status, body: {} } })
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
    'GET /api/cashier/v1/offerings': { status: 404, body: { code: 'offering_not_found', error: 'no offering configured' } },
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
  const fetch = fakeFetch({ 'GET /api/cashier/v1/offerings': { status: 402, body: {} } })
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
    'GET /api/cashier/v1/offerings': {
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

test('offerings sends the app user id (for experiments) and reads the paywall flow, its id and the experiment', async () => {
  const flow = { version: 2, theme: {}, initial: 's1', screens: [{ id: 's1', name: 'One', blocks: [] }] }
  const fetch = fakeFetch({ 'GET /api/cashier/v1/offerings': { body: { current_offering: 'b', packages: [], paywall: flow, paywall_id: 12, experiment: { id: 3, variant: 'b' } } } })
  const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'user 1', fetch, storage: memoryStorage() })

  const o = await cashier.offerings()
  assert.equal(fetch.calls[0].key, 'GET /api/cashier/v1/offerings?app_user_id=user%201')
  assert.deepEqual(o.paywall, flow)
  assert.equal(o.paywallId, 12)
  assert.deepEqual(o.experiment, { id: 3, variant: 'b' })
})

test('presentPaywall reports the view, answers, and a close without a purchase', async () => {
  const { Window } = await import('happy-dom')
  const win = new Window()
  const prev = globalThis.document
  globalThis.document = win.document
  try {
    const flow = { version: 2, theme: {}, initial: 'ask', screens: [
      { id: 'ask', name: 'Ask', question: 'How did you hear?', blocks: [
        { id: 'o1', type: 'button', variant: 'option', text: 'A friend', answer: 'friend', action: 'close' },
      ] },
    ] }
    const fetch = fakeFetch({
      'GET /api/cashier/v1/offerings': { body: { current_offering: 'd', packages: [], paywall: flow, paywall_id: 7, experiment: { id: 2, variant: 'a' } } },
      'POST /api/cashier/v1/paywalls/events': { status: 204 },
      'POST /api/cashier/v1/paywalls/responses': { status: 204 },
    })
    const cashier = new CashierCloud({ apiKey: 'k', appUserId: 'u1', fetch, storage: memoryStorage() })
    const answers = []
    const shown = cashier.presentPaywall({ onAnswer: (a) => answers.push(a), locale: 'en' })
    await new Promise((r) => setTimeout(r, 10))
    const btn = [...win.document.querySelectorAll('button')].find((b) => b.textContent.includes('A friend'))
    btn.click()
    const res = await shown
    await new Promise((r) => setTimeout(r, 10))
    assert.equal(res.purchased, false)
    assert.deepEqual(answers, [{ screen_id: 'ask', question: 'How did you hear?', answer: 'friend' }])
    const posts = fetch.calls.filter((c) => c.key.startsWith('POST')).map((c) => [c.key, c.body])
    assert.deepEqual(posts, [
      ['POST /api/cashier/v1/paywalls/events', { app_user_id: 'u1', paywall_id: 7, type: 'view', experiment_id: 2, variant: 'a' }],
      ['POST /api/cashier/v1/paywalls/responses', { app_user_id: 'u1', paywall_id: 7, screen_id: 'ask', question: 'How did you hear?', answer: 'friend' }],
      ['POST /api/cashier/v1/paywalls/events', { app_user_id: 'u1', paywall_id: 7, type: 'close', experiment_id: 2, variant: 'a' }],
    ])
  } finally {
    globalThis.document = prev
  }
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
