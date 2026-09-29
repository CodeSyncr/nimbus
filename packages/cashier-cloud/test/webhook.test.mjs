import assert from 'node:assert/strict'
import test from 'node:test'
import { createHmac } from 'node:crypto'

const { verifyCashierWebhook } = await import('../dist/index.js')

// Signed the way the Cashier server signs (SignWebhook in webhooks.go),
// computed here independently with Node's own HMAC.
const secret = 'whsec_test'
const body = JSON.stringify({ api_version: '1', event: { id: 'e1', type: 'renewal', app_user_id: 'u1', entitlement_ids: ['premium'], expiration_at_ms: null, event_timestamp_ms: 1 } })
const sign = (t, b = body, key = secret) => `t=${t},v1=${createHmac('sha256', key).update(`${t}.${b}`).digest('hex')}`
const now = 1_800_000_000_000
const t = Math.floor(now / 1000)

test('a genuine delivery verifies and parses', async () => {
  const hook = await verifyCashierWebhook(secret, sign(t), body, 300, now)
  assert.equal(hook.event.type, 'renewal')
  assert.equal(hook.event.app_user_id, 'u1')
  assert.deepEqual(hook.event.entitlement_ids, ['premium'])
  // Bytes work as well as strings.
  assert.ok(await verifyCashierWebhook(secret, sign(t), new TextEncoder().encode(body), 300, now))
})

test('tampered, stale, wrongly keyed or unsigned deliveries are refused', async () => {
  assert.equal(await verifyCashierWebhook(secret, sign(t), body.replace('renewal', 'expiration'), 300, now), null)
  assert.equal(await verifyCashierWebhook(secret, sign(t - 301), body, 300, now), null)
  assert.equal(await verifyCashierWebhook(secret, sign(t + 301), body, 300, now), null)
  assert.equal(await verifyCashierWebhook(secret, sign(t, body, 'other'), body, 300, now), null)
  assert.equal(await verifyCashierWebhook(secret, undefined, body, 300, now), null)
  assert.equal(await verifyCashierWebhook(secret, 't=abc,v1=00', body, 300, now), null)
  assert.equal(await verifyCashierWebhook('', sign(t), body, 300, now), null)
})
