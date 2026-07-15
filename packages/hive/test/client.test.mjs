import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHive } from '../dist/index.js';

const registry = {
  'posts.index': { method: 'GET', path: '/posts', params: {} },
  'posts.show': { method: 'GET', path: '/posts/:id', params: { id: 'string' } },
  'posts.store': { method: 'POST', path: '/posts', params: {} },
  // Brace syntax (as emitted by `nimbus gen:client` for /sessions/{id} routes)
  'sessions.show': { method: 'GET', path: '/api/sessions/{id}', params: { id: 'string' } },
  'diffs.approve': {
    method: 'POST',
    path: '/api/sessions/{id}/diffs/{diffId}/approve',
    params: { id: 'string', diffId: 'string' },
  },
  // Prefix-collision trap: ":id" must not match inside ":idx"
  'tricky.show': { method: 'GET', path: '/t/:id/:idx', params: { id: 'string', idx: 'string' } },
};

const json = (obj, status = 200, headers = {}) =>
  new Response(JSON.stringify(obj), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });

function stubFetch(handler) {
  const calls = [];
  globalThis.fetch = async (url, init) => {
    calls.push({ url: String(url), init });
    return handler(calls.length, url, init);
  };
  return calls;
}

test('retries a 503 then succeeds (idempotent GET)', async () => {
  const calls = stubFetch(n => (n === 1 ? json({}, 503) : json({ ok: true })));
  const client = createHive({ baseUrl: 'http://x', registry, retry: { limit: 2, backoffLimit: 1 } });
  const data = await client.api.posts.index({});
  assert.equal(calls.length, 2);
  assert.deepEqual(data, { ok: true });
});

test('does NOT retry a non-idempotent POST', async () => {
  const calls = stubFetch(() => json({ error: 'boom' }, 500));
  const client = createHive({ baseUrl: 'http://x', registry, retry: { limit: 3, backoffLimit: 1 } });
  const [data, err] = await client.api.posts.store({ body: { title: 'x' } }).safe();
  assert.equal(calls.length, 1);
  assert.equal(data, null);
  assert.equal(err.kind, 'http');
  assert.equal(err.status, 500);
});

test('retries a network error then succeeds', async () => {
  const calls = stubFetch(n => {
    if (n === 1) throw new TypeError('Failed to fetch');
    return json({ recovered: true });
  });
  const client = createHive({ baseUrl: 'http://x', registry, retry: { limit: 1, backoffLimit: 1 } });
  const data = await client.api.posts.index({});
  assert.equal(calls.length, 2);
  assert.deepEqual(data, { recovered: true });
});

test('honors Retry-After and fires onRetry', async () => {
  const seen = [];
  const calls = stubFetch(n => (n === 1 ? json({}, 429, { 'retry-after': '0' }) : json({ ok: 1 })));
  const client = createHive({
    baseUrl: 'http://x',
    registry,
    retry: {
      limit: 1,
      backoffLimit: 5,
      onRetry: info => seen.push(info),
    },
  });
  await client.api.posts.index({});
  assert.equal(calls.length, 2);
  assert.equal(seen.length, 1);
  assert.equal(seen[0].attempt, 1);
  assert.equal(seen[0].delay, 0); // Retry-After: 0
  assert.equal(seen[0].error.status, 429);
});

test('serializes array query params as repeated keys', async () => {
  const calls = stubFetch(() => json({}));
  const client = createHive({ baseUrl: 'http://x', registry });
  await client.api.posts.index({ query: { tag: ['a', 'b'], page: 2, skip: null } });
  const url = calls[0].url;
  assert.ok(url.includes('tag=a&tag=b'), url);
  assert.ok(url.includes('page=2'), url);
  assert.ok(!url.includes('skip'), url);
});

test('url() builds path params + array query', () => {
  const client = createHive({ baseUrl: 'http://x', registry });
  const u = client.url('posts.show', { id: '7' }, { tag: ['a', 'b'] });
  assert.equal(u, 'http://x/posts/7?tag=a&tag=b');
});

test('substitutes {brace} path params', async () => {
  const calls = stubFetch(() => json({}));
  const client = createHive({ baseUrl: 'http://x', registry });
  await client.api.sessions.show({ params: { id: 'abc' } });
  assert.equal(calls[0].url, 'http://x/api/sessions/abc');
});

test('substitutes multiple {brace} params', () => {
  const client = createHive({ baseUrl: 'http://x', registry });
  const u = client.url('diffs.approve', { id: 's1', diffId: 'd9' });
  assert.equal(u, 'http://x/api/sessions/s1/diffs/d9/approve');
});

test('":id" does not clobber the ":idx" prefix', () => {
  const client = createHive({ baseUrl: 'http://x', registry });
  const u = client.url('tricky.show', { id: 'A', idx: 'B' });
  assert.equal(u, 'http://x/t/A/B');
});

test('encodes param values', () => {
  const client = createHive({ baseUrl: 'http://x', registry });
  const u = client.url('posts.show', { id: 'a b/c' });
  assert.equal(u, 'http://x/posts/a%20b%2Fc');
});

test('.safe() returns [data, null] on success and http error is narrowable', async () => {
  stubFetch(() => json({ errors: [{ field: 'email', message: 'required' }] }, 422));
  const client = createHive({ baseUrl: 'http://x', registry });
  const [data, err] = await client.api.posts.store({ body: {} }).safe();
  assert.equal(data, null);
  assert.equal(err.isValidationError(), true);
  assert.equal(err.isStatus(422), true);
  assert.equal(err.isStatus(404), false);
});

test('caller AbortSignal cancels the request', async () => {
  stubFetch((_n, _url, init) => {
    // Reject when the (combined) signal aborts, like real fetch.
    return new Promise((_resolve, reject) => {
      init.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    });
  });
  const client = createHive({ baseUrl: 'http://x', registry });
  const ac = new AbortController();
  const p = client.api.posts.index({ signal: ac.signal }).safe();
  ac.abort();
  const [data, err] = await p;
  assert.equal(data, null);
  assert.equal(err.kind, 'network');
});
