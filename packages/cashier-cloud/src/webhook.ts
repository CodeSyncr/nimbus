/**
 * Cashier webhooks, for your backend (Node 18+, Deno, Bun, edge runtimes —
 * anywhere with Web Crypto).
 *
 * Cashier POSTs every subscriber event as JSON with a signature header:
 *
 *     Cashier-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>
 *
 * Verify it over the RAW body (before any JSON parsing), then parse:
 *
 *     const event = await verifyCashierWebhook(secret, req.headers['cashier-signature'], rawBody)
 *     if (!event) return res.status(400).end()
 */

/** Event types Cashier sends. */
export type CashierWebhookType =
  | 'initial_purchase' | 'renewal' | 'non_renewing_purchase' | 'cancellation' | 'uncancellation'
  | 'expiration' | 'billing_issue' | 'product_change' | 'subscription_paused' | 'subscription_extended'
  | 'promotional_grant' | 'promotional_revoke' | 'transfer'
  | 'paywall_response' | 'test'

/** One subscriber event, exactly as Cashier sends it (snake_case). */
export interface CashierWebhookEvent {
  id: string
  type: CashierWebhookType
  /** Your user id. */
  app_user_id: string
  /** transfer: the id the purchase moved from. */
  from_app_user_id?: string
  product_id?: string
  /** product_change: the product before. */
  old_product_id?: string
  entitlement_ids: string[]
  period_type?: 'trial' | 'intro' | 'normal' | 'promotional' | 'grace'
  /** cancellation / expiration: why, e.g. "unsubscribe", "billing_error", "refund". */
  reason?: string
  /** app_store | play_store | promotional | … */
  store?: string
  environment?: 'production' | 'sandbox'
  transaction_id?: string
  original_transaction_id?: string
  /** Price in millionths of a unit (4_990_000 = 4.99). */
  price_micros?: number
  currency?: string
  /** When access ends, in ms since the epoch; null for lifetime or none. */
  /** paywall_response: which paywall asked, what, and the answer. */
  paywall_id?: number
  question?: string
  answer?: string
  expiration_at_ms: number | null
  event_timestamp_ms: number
}

/** The body of a Cashier webhook delivery. */
export interface CashierWebhook {
  api_version: string
  event: CashierWebhookEvent
}

const hex = (buf: ArrayBuffer) => Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, '0')).join('')

/** Constant-time comparison of two strings. */
function same(a: string, b: string): boolean {
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i)
  return diff === 0
}

/**
 * Checks a delivery's Cashier-Signature header against your app's webhook
 * secret and returns the parsed payload — or null if the signature is wrong,
 * missing, or older than `toleranceSeconds` (default 5 minutes).
 *
 * Pass the body exactly as received (string or bytes); re-serialised JSON
 * will not match.
 */
export async function verifyCashierWebhook(
  secret: string,
  signatureHeader: string | null | undefined,
  rawBody: string | Uint8Array,
  toleranceSeconds = 300,
  now: number = Date.now(),
): Promise<CashierWebhook | null> {
  if (!secret || !signatureHeader) return null
  let t = '', v1 = ''
  for (const part of signatureHeader.split(',')) {
    const [k, v] = part.trim().split('=', 2)
    if (k === 't') t = v ?? ''
    if (k === 'v1') v1 = v ?? ''
  }
  const sec = Number(t)
  if (!t || !v1 || !Number.isFinite(sec) || Math.abs(now / 1000 - sec) > toleranceSeconds) return null
  const body = typeof rawBody === 'string' ? rawBody : new TextDecoder().decode(rawBody)
  const subtle = (globalThis as any).crypto?.subtle as SubtleCrypto | undefined
  if (!subtle) throw new Error('cashier-cloud: verifyCashierWebhook needs Web Crypto (Node 18+ or a modern runtime)')
  const enc = new TextEncoder()
  const key = await subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const want = hex(await subtle.sign('HMAC', key, enc.encode(`${t}.${body}`)))
  if (!same(want, v1.toLowerCase())) return null
  try {
    return JSON.parse(body) as CashierWebhook
  } catch {
    return null
  }
}
