import { test } from 'node:test';
import assert from 'node:assert/strict';
import { NimbusDocs, NimbusDocsError } from '../dist/index.js';

/** A fetch stand-in that records calls and replays canned responses. */
function stubFetch(responses) {
  const calls = [];
  const queue = [...responses];
  const fn = async (url, init) => {
    calls.push({ url, init });
    const next = queue.shift() ?? { status: 200, body: {} };
    if (next.throw) throw next.throw;
    return {
      ok: next.status >= 200 && next.status < 300,
      status: next.status,
      json: async () => next.body,
    };
  };
  fn.calls = calls;
  return fn;
}

const opts = (fetch) => ({ apiKey: 'sgm_live_abc', product: 'sigma', fetch });

test('rejects a missing or wrong product', () => {
  assert.throws(() => new NimbusDocs({ apiKey: 'k', product: 'chat' }), NimbusDocsError);
  assert.throws(() => new NimbusDocs({ product: 'sigma' }), NimbusDocsError);
});

test('sends the key as a bearer token to the product path', async () => {
  const fetch = stubFetch([{ status: 200, body: { documents: [] } }]);
  await new NimbusDocs(opts(fetch)).list();

  const { url, init } = fetch.calls[0];
  assert.match(url, /^https:\/\/nimbusgo\.space\/api\/v1\/sigma\/documents/);
  assert.equal(init.headers.Authorization, 'Bearer sgm_live_abc');
});

test('upload passes link options as query and idempotency as a header', async () => {
  const fetch = stubFetch([{ status: 201, body: { id: 1, url: 'https://nimbusgo.space/v/sigma/1?sig=x' } }]);
  const doc = await new NimbusDocs(opts(fetch)).upload({
    content: 'a,b\n1,2',
    filename: 'q3.csv',
    ttlSeconds: 3600,
    mode: 'view',
    retainDays: 30,
    idempotencyKey: 'order-99',
  });

  const { url, init } = fetch.calls[0];
  assert.match(url, /ttl=3600/);
  assert.match(url, /mode=view/);
  assert.match(url, /retain_days=30/);
  assert.equal(init.headers['Idempotency-Key'], 'order-99');
  assert.equal(JSON.parse(init.body).filename, 'q3.csv');
  // Title defaults to the filename rather than being left empty.
  assert.equal(JSON.parse(init.body).title, 'q3.csv');
  assert.equal(doc.url, 'https://nimbusgo.space/v/sigma/1?sig=x');
});

test('binary content is accepted', async () => {
  const fetch = stubFetch([{ status: 201, body: { id: 2 } }]);
  await new NimbusDocs(opts(fetch)).upload({
    content: new TextEncoder().encode('hello'),
    filename: 'a.txt',
  });
  assert.equal(JSON.parse(fetch.calls[0].init.body).content, 'hello');
});

test('a quota refusal carries the quota, not just a message', async () => {
  const quota = { limit: 50, used: 51, remaining: 0, overage: 1, allowed: false, period: '2026-09' };
  const fetch = stubFetch([{ status: 402, body: { error: 'allowance used up', quota } }]);

  await assert.rejects(
    () => new NimbusDocs(opts(fetch)).upload({ content: 'x', filename: 'a.csv' }),
    (err) => {
      assert.ok(err instanceof NimbusDocsError);
      assert.equal(err.code, 'quota_exceeded');
      assert.equal(err.status, 402);
      assert.equal(err.quota.used, 51);
      return true;
    },
  );
});

test('http statuses map to stable codes', async () => {
  const cases = [[401, 'unauthorized'], [404, 'not_found'], [413, 'too_large'], [429, 'rate_limited'], [500, 'server_error']];
  for (const [status, code] of cases) {
    const fetch = stubFetch([{ status, body: { error: 'nope' } }]);
    await assert.rejects(
      () => new NimbusDocs(opts(fetch)).get(1),
      (err) => err.code === code,
      `status ${status} should map to ${code}`,
    );
  }
});

test('a network failure is reported as one, not as a crash', async () => {
  const fetch = stubFetch([{ throw: new Error('ECONNREFUSED') }]);
  await assert.rejects(
    () => new NimbusDocs(opts(fetch)).usage(),
    (err) => err.code === 'network_error' && /ECONNREFUSED/.test(err.message),
  );
});

test('a timeout is distinguishable from a network failure', async () => {
  const abort = new Error('aborted');
  abort.name = 'AbortError';
  const fetch = stubFetch([{ throw: abort }]);
  await assert.rejects(
    () => new NimbusDocs(opts(fetch)).usage(),
    (err) => err.code === 'timeout',
  );
});

test('re-minting a link hits the link endpoint for that document', async () => {
  const fetch = stubFetch([{ status: 200, body: { url: 'https://x/v/sigma/7?sig=y', mode: 'edit', expires_at: '2026-09-09T00:00:00Z' } }]);
  const link = await new NimbusDocs(opts(fetch)).link(7, { ttlSeconds: 60, mode: 'edit' });

  assert.match(fetch.calls[0].url, /\/documents\/7\/link\?/);
  assert.match(fetch.calls[0].url, /mode=edit/);
  assert.equal(link.mode, 'edit');
});

test('baseUrl override targets a local deployment', async () => {
  const fetch = stubFetch([{ status: 200, body: {} }]);
  await new NimbusDocs({ ...opts(fetch), baseUrl: 'http://localhost:3333/' }).usage();
  assert.match(fetch.calls[0].url, /^http:\/\/localhost:3333\/api\/v1\/sigma\/usage$/);
});
