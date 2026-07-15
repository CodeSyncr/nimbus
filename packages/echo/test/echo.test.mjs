import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { Echo, Channel, PrivateChannel, PresenceChannel } from '../dist/index.mjs';

// ── Test doubles ───────────────────────────────────────────────────
// Echo opens an SSE stream via `new EventSource(url)` and POSTs
// subscribe/unsubscribe via fetch. Both are stubbed so the whole
// lifecycle can be driven deterministically with no server.

class FakeEventSource {
  static last = null;
  static instances = [];
  constructor(url) {
    this.url = url;
    this.closed = false;
    FakeEventSource.last = this;
    FakeEventSource.instances.push(this);
  }
  close() {
    this.closed = true;
  }
  // Helpers for tests to drive the stream:
  open() {
    this.onopen?.();
  }
  emit(obj) {
    this.onmessage?.({ data: JSON.stringify(obj) });
  }
  emitRaw(data) {
    this.onmessage?.({ data });
  }
  fail(err = new Error('boom')) {
    this.onerror?.(err);
  }
}

let fetchCalls = [];

beforeEach(() => {
  FakeEventSource.last = null;
  FakeEventSource.instances = [];
  fetchCalls = [];
  globalThis.EventSource = FakeEventSource;
  globalThis.fetch = async (url, init) => {
    fetchCalls.push({ url: String(url), init, body: init?.body ? JSON.parse(init.body) : undefined });
    return new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } });
  };
});

const tick = () => new Promise(r => setTimeout(r, 0));

// Connects an Echo instance: open the stream and deliver the server's UID frame.
async function handshake(uid = 'uid-1') {
  FakeEventSource.last.open();
  FakeEventSource.last.emit({ uid });
  await tick();
}

// ── Config ─────────────────────────────────────────────────────────

test('strips a trailing slash from baseURL and defaults the transmit path', () => {
  const echo = new Echo({ baseURL: 'http://localhost:3333/' });
  echo.channel('news');
  assert.equal(FakeEventSource.last.url, 'http://localhost:3333/__transmit/events');
});

test('honors a custom transmit path', () => {
  const echo = new Echo({ baseURL: 'http://x', path: 'rt' });
  echo.channel('news');
  assert.equal(FakeEventSource.last.url, 'http://x/rt/events');
});

// ── Channel naming ─────────────────────────────────────────────────

test('channel types apply the right name prefixes', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  assert.ok(echo.channel('news') instanceof Channel);
  assert.ok(echo.private('projects.1') instanceof PrivateChannel);
  assert.ok(echo.join('room.1') instanceof PresenceChannel);
  await handshake();

  // The prefixed names are what actually gets subscribed on the server.
  const subscribed = fetchCalls.map(c => c.body.channel).sort();
  assert.deepEqual(subscribed, ['news', 'presence-room.1', 'private-projects.1']);
});

test('private() and join() are aliases that share prefixing with private_()', () => {
  const echo = new Echo({ baseURL: 'http://x' });
  assert.ok(echo.private_('a') instanceof PrivateChannel);
  assert.ok(echo.private('b') instanceof PrivateChannel);
});

// ── Connection & subscribe handshake ───────────────────────────────

test('subscribes over POST once the UID frame arrives', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  echo.channel('news');

  // Before the UID frame, no subscribe POST should have been sent.
  await tick();
  assert.equal(fetchCalls.length, 0);

  await handshake('uid-9');

  assert.equal(fetchCalls.length, 1);
  assert.equal(fetchCalls[0].url, 'http://x/__transmit/subscribe');
  assert.deepEqual(fetchCalls[0].body, { uid: 'uid-9', channel: 'news' });
  assert.equal(echo.getUid(), 'uid-9');
  assert.equal(echo.isConnected(), true);
});

test('sends bearer token and csrf token when configured', async () => {
  const echo = new Echo({ baseURL: 'http://x', bearerToken: 'tok', csrfToken: 'csrf-1' });
  echo.private('projects.1');
  await handshake();

  const call = fetchCalls[0];
  assert.equal(call.init.headers['Authorization'], 'Bearer tok');
  assert.equal(call.body.channel, 'private-projects.1');
  assert.equal(call.body.csrf_token, 'csrf-1');
  assert.equal(call.init.credentials, 'include');
});

test('setBearerToken applies to later subscribes', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  echo.setBearerToken('later');
  echo.channel('news');
  await handshake();
  assert.equal(fetchCalls[0].init.headers['Authorization'], 'Bearer later');
});

test('subscribing twice to the same channel only sends one POST', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  echo.channel('news');
  echo.channel('news');
  await handshake();
  assert.equal(fetchCalls.filter(c => c.url.endsWith('/subscribe')).length, 1);
});

// ── Event dispatch ─────────────────────────────────────────────────

test('listen() receives the event payload', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  echo.channel('news').listen('NewMessage', d => seen.push(d));
  await handshake();

  FakeEventSource.last.emit({ channel: 'news', event: 'NewMessage', payload: { body: 'hi' } });
  assert.deepEqual(seen, [{ body: 'hi' }]);
});

test('listeners are scoped to their own channel and event', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  echo.channel('news').listen('A', () => seen.push('news:A'));
  echo.channel('other').listen('A', () => seen.push('other:A'));
  await handshake();

  FakeEventSource.last.emit({ channel: 'news', event: 'A', payload: {} });
  FakeEventSource.last.emit({ channel: 'news', event: 'B', payload: {} }); // no listener
  assert.deepEqual(seen, ['news:A']);
});

test('listenAll() receives every event wrapped as { event, data }', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  echo.channel('news').listenAll(e => seen.push(e));
  await handshake();

  FakeEventSource.last.emit({ channel: 'news', event: 'X', payload: { n: 1 } });
  assert.deepEqual(seen, [{ event: 'X', data: { n: 1 } }]);
});

test('stopListening(event, cb) actually stops that callback firing', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  const cb = d => seen.push(d);
  const ch = echo.channel('news').listen('E', cb);
  await handshake();

  FakeEventSource.last.emit({ channel: 'news', event: 'E', payload: { n: 1 } });
  assert.deepEqual(seen, [{ n: 1 }]); // fires before removal

  ch.stopListening('E', cb);
  FakeEventSource.last.emit({ channel: 'news', event: 'E', payload: { n: 2 } });
  assert.deepEqual(seen, [{ n: 1 }]); // silent after removal
});

test('stopListening(event) removes every listener for that event', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  const ch = echo.channel('news');
  ch.listen('E', () => seen.push('a'));
  ch.listen('E', () => seen.push('b'));
  await handshake();

  ch.stopListening('E');
  FakeEventSource.last.emit({ channel: 'news', event: 'E', payload: {} });
  assert.deepEqual(seen, []);
});

test('stopListening leaves other events and channels untouched', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  const news = echo.channel('news');
  news.listen('A', () => seen.push('news:A'));
  news.listen('B', () => seen.push('news:B'));
  echo.channel('other').listen('A', () => seen.push('other:A'));
  await handshake();

  news.stopListening('A');
  const es = FakeEventSource.last;
  es.emit({ channel: 'news', event: 'A', payload: {} });   // removed
  es.emit({ channel: 'news', event: 'B', payload: {} });   // kept
  es.emit({ channel: 'other', event: 'A', payload: {} });  // kept
  assert.deepEqual(seen, ['news:B', 'other:A']);
});

test('a throwing listener does not stop the others', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const seen = [];
  const ch = echo.channel('news');
  ch.listen('E', () => { throw new Error('listener blew up'); });
  ch.listen('E', () => seen.push('second ran'));
  await handshake();

  const origErr = console.error;
  console.error = () => {}; // silence the expected [Echo] Listener error log
  FakeEventSource.last.emit({ channel: 'news', event: 'E', payload: {} });
  console.error = origErr;

  assert.deepEqual(seen, ['second ran']);
});

test('ignores ping frames and non-JSON messages', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  echo.channel('news').listenAll(() => assert.fail('should not dispatch'));
  await handshake();

  FakeEventSource.last.emit({ type: 'ping' });
  FakeEventSource.last.emitRaw('not json at all');
  // reaching here without throwing is the assertion
});

// ── Presence channels ──────────────────────────────────────────────

test('presence here/joining/leaving map the __presence events', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  const events = [];
  echo.join('room.1')
    .here(users => events.push(['here', users]))
    .joining(u => events.push(['joining', u]))
    .leaving(u => events.push(['leaving', u]));
  await handshake();

  const es = FakeEventSource.last;
  es.emit({ channel: 'presence-room.1', event: '__presence:here', payload: { users: [{ id: 1 }] } });
  es.emit({ channel: 'presence-room.1', event: '__presence:joining', payload: { user: { id: 2 } } });
  es.emit({ channel: 'presence-room.1', event: '__presence:leaving', payload: { user: { id: 2 } } });

  assert.deepEqual(events, [
    ['here', [{ id: 1 }]],
    ['joining', { id: 2 }],
    ['leaving', { id: 2 }],
  ]);
});

test('presence here() defaults to an empty array when users is absent', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  let got = null;
  echo.join('room.1').here(users => { got = users; });
  await handshake();
  FakeEventSource.last.emit({ channel: 'presence-room.1', event: '__presence:here', payload: {} });
  assert.deepEqual(got, []);
});

// ── Unsubscribe / leave / disconnect ───────────────────────────────

test('leave() unsubscribes public, private and presence variants', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  echo.channel('room.1');
  echo.private('room.1');
  echo.join('room.1');
  await handshake();
  fetchCalls = [];

  echo.leave('room.1');
  await tick();

  const unsubbed = fetchCalls.filter(c => c.url.endsWith('/unsubscribe')).map(c => c.body.channel);
  assert.deepEqual(unsubbed.sort(), ['presence-room.1', 'private-room.1', 'room.1']);
});

test('disconnects and closes the stream when the last channel is left', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  let disconnected = false;
  echo.onDisconnect(() => { disconnected = true; });
  echo.channel('news');
  await handshake();

  const es = FakeEventSource.last;
  echo.leave('news');
  await tick();

  assert.equal(es.closed, true);
  assert.equal(echo.isConnected(), false);
  assert.equal(echo.getUid(), '');
  assert.equal(disconnected, true);
});

test('disconnect() clears state and fires onDisconnect', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  let disconnected = 0;
  echo.onDisconnect(() => disconnected++);
  echo.channel('news');
  await handshake();

  echo.disconnect();
  assert.equal(echo.isConnected(), false);
  assert.equal(echo.getUid(), '');
  assert.equal(disconnected, 1);
});

// ── Connection callbacks & reconnect ───────────────────────────────

test('onConnect fires when the stream opens', async () => {
  const echo = new Echo({ baseURL: 'http://x' });
  let connected = 0;
  echo.onConnect(() => connected++);
  echo.channel('news');
  FakeEventSource.last.open();
  assert.equal(connected, 1);
  assert.equal(echo.isConnected(), true);
});

test('onError fires and auto-reconnect opens a new stream', async () => {
  const echo = new Echo({ baseURL: 'http://x', reconnectDelay: 1 });
  let errors = 0;
  echo.onError(() => errors++);
  echo.channel('news');
  await handshake();

  assert.equal(FakeEventSource.instances.length, 1);
  FakeEventSource.last.fail();
  assert.equal(errors, 1);
  assert.equal(echo.isConnected(), false);

  await new Promise(r => setTimeout(r, 20));
  assert.equal(FakeEventSource.instances.length, 2); // reconnected
});

test('autoReconnect:false does not reopen the stream', async () => {
  const echo = new Echo({ baseURL: 'http://x', autoReconnect: false, reconnectDelay: 1 });
  echo.channel('news');
  await handshake();
  FakeEventSource.last.fail();
  await new Promise(r => setTimeout(r, 20));
  assert.equal(FakeEventSource.instances.length, 1);
});

test('maxReconnectAttempts caps reconnection', async () => {
  const echo = new Echo({ baseURL: 'http://x', reconnectDelay: 1, maxReconnectAttempts: 2 });
  echo.channel('news');
  await handshake();

  for (let i = 0; i < 5; i++) {
    FakeEventSource.last.fail();
    await new Promise(r => setTimeout(r, 15));
  }
  // 1 initial + at most 2 reconnects
  assert.ok(FakeEventSource.instances.length <= 3, `opened ${FakeEventSource.instances.length} streams`);
});
