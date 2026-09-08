import { test, before } from 'node:test';
import assert from 'node:assert/strict';

// A DOM small enough to read in one go — just what embedDocument touches.
class El {
  constructor(tag) { this.tagName = tag; this.children = []; this.style = {}; this.attrs = {}; this.listeners = {}; this.parentNode = null; this.src = ''; this.className = ''; this.title = ''; }
  setAttribute(k, v) { this.attrs[k] = v; }
  appendChild(c) { c.parentNode = this; this.children.push(c); return c; }
  addEventListener(t, fn) { (this.listeners[t] ||= []).push(fn); }
  remove() { if (this.parentNode) this.parentNode.children = this.parentNode.children.filter((c) => c !== this); this.parentNode = null; }
  get contentWindow() { return { postMessage() {}, location: { reload: () => { this.reloaded = (this.reloaded || 0) + 1; } } }; }
}
before(() => {
  globalThis.document = { createElement: (t) => new El(t), querySelector: () => null };
  globalThis.window = { addEventListener() {}, removeEventListener() {} };
});

test('svelte action mounts, reloads on url change, destroys', async () => {
  const { nimbusDocument } = await import('../dist/svelte.js');
  const node = new El('div');
  const action = nimbusDocument(node, { url: 'https://x/v/sigma/1?sig=a', height: 'auto' });
  assert.equal(node.children.length, 1);
  const frame = node.children[0];
  assert.equal(frame.tagName, 'iframe');
  assert.equal(frame.src, 'https://x/v/sigma/1?sig=a');
  assert.match(frame.attrs.sandbox, /allow-scripts/);

  action.update({ url: 'https://x/v/sigma/1?sig=b', height: 'auto' });
  assert.equal(frame.src, 'https://x/v/sigma/1?sig=b', 'a new url reloads the frame in place');
  action.destroy();
  assert.equal(node.children.length, 0, 'destroy removes the frame');
});

test('svelte action refuses a key-shaped url', async () => {
  const { nimbusDocument } = await import('../dist/svelte.js');
  assert.throws(() => nimbusDocument(new El('div'), { url: 'https://x/?api_key=sgm_live_1' }), /API key/);
});

test('react component renders a host element on the server', async () => {
  const { createElement } = await import('react');
  const { renderToString } = await import('react-dom/server');
  const { NimbusDocument } = await import('../dist/react.js');
  const html = renderToString(createElement(NimbusDocument, { url: 'https://x/v/carbon/2?sig=c' }));
  assert.match(html, /^<div/);
});

test('vue component renders a host element on the server', async () => {
  const { createSSRApp, h } = await import('vue');
  const { renderToString } = await import('vue/server-renderer');
  const { NimbusDocument } = await import('../dist/vue.js');
  const html = await renderToString(createSSRApp({ render: () => h(NimbusDocument, { url: 'https://x/v/carbon/3?sig=d' }) }));
  assert.match(html, /^<div/);
});
