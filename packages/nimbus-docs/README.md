# @codesyncr/nimbus-docs

Upload spreadsheets and documents to Nimbus Cloud, get a signed link back, and
embed it in your page.

Two entry points, deliberately separated:

- **`@codesyncr/nimbus-docs`** — server side. Holds your API key. Refuses to
  construct itself in a browser.
- **`@codesyncr/nimbus-docs/embed`** — browser side. Takes a signed URL. Never
  sees a key.

## Install

```bash
npm install @codesyncr/nimbus-docs
```

## On your server

```js
import { NimbusDocs } from '@codesyncr/nimbus-docs';

const sigma = new NimbusDocs({
  apiKey: process.env.SIGMA_API_KEY,   // sgm_live_…
  product: 'sigma',
});

const doc = await sigma.upload({
  content: csvString,
  filename: 'q3-revenue.csv',
  title: 'Q3 revenue',
  ttlSeconds: 900,          // how long the returned link lives (default 15m)
  idempotencyKey: order.id, // a retry returns the same document, not a second one
});

// Hand only this to the browser.
res.json({ url: doc.url });
```

Carbon works the same way with a `cbn_live_…` key and `product: 'carbon'`.

### The rest of the surface

```js
await sigma.list(50);                              // newest first
await sigma.get(id);                               // metadata
await sigma.link(id, { ttlSeconds: 3600 });        // re-mint an expired link
await sigma.delete(id);
await sigma.revokeAllLinks();                      // kill every outstanding link
await sigma.usage();                               // this month's allowance
```

### Errors

Every failure is a `NimbusDocsError` with a stable `code`, so you can branch
without matching on prose:

`unauthorized` · `quota_exceeded` · `not_found` · `too_large` ·
`invalid_request` · `rate_limited` · `server_error` · `network_error` ·
`timeout`

```js
try {
  await sigma.upload({ content, filename });
} catch (err) {
  if (err.code === 'quota_exceeded') {
    // err.quota carries used / limit / remaining for this period
  }
}
```

## Framework bindings

Thin wrappers over `embedDocument`, one per entry point. Each owns one
iframe: a new `url` reloads it in place (the answer to a re-minted link),
a new `theme` is pushed to it, and unmounting destroys it.

```jsx
// React
import { NimbusDocument } from '@codesyncr/nimbus-docs/react';
<NimbusDocument url={signedUrl} height="auto" onReady={(doc) => ...} />
```

```vue
<!-- Vue 3 -->
<script setup>import { NimbusDocument } from '@codesyncr/nimbus-docs/vue';</script>
<NimbusDocument :url="signedUrl" height="auto" @ready="onReady" />
```

```svelte
<!-- Svelte 4 or 5: an action, so no compiler step is needed -->
<script>import { nimbusDocument } from '@codesyncr/nimbus-docs/svelte';</script>
<div use:nimbusDocument={params}></div>
```

React and Vue are optional peer dependencies; the Svelte action has none.
Astro, Next.js, Nuxt and SvelteKit recipes — and Go, Nimbus, Laravel,
Django and Rails for the server half — are in the hosted docs at
`nimbusgo.space/cloud/docs/sigma` and `/carbon`.

## In the browser

```js
import { embedDocument } from '@codesyncr/nimbus-docs/embed';

const doc = embedDocument({
  url: signedUrlFromYourServer,
  target: '#report',
  height: 'auto',            // follows the document
  theme: 'light',
  onReady: (info) => console.log('showing', info.title),
  onResize: (h) => {},
  onError: (e) => console.warn(e.message),
});

doc.setTheme('dark');
doc.reload(freshUrl);        // after re-minting an expired link
doc.destroy();
```

### Allowing your site to embed

A document may only be framed by origins you name. Set them on the key that
creates the document — Nimbus Cloud dashboard → **API & CLI Keys** → *Document
API keys* → **Embed origins**:

```
https://app.example.com, https://example.com
```

Scheme and host only. A key with no origins produces documents that open
directly but refuse to be framed anywhere; that is the default, so a
misplaced link cannot quietly appear inside someone else's site.

Origins are recorded on each document when it is created, so re-scoping a key
later never breaks pages that already embed older documents.

## The message contract

Messages the frame sends to your page (`source: "nimbus-docs"`):

| type | payload | when |
|---|---|---|
| `ready` | `document` — id, product, title, filename, size, mode | the document is displayed |
| `resize` | `height` in pixels | on load and whenever the content resizes |
| `error` | `message` | the document could not be displayed |

Messages your page can send (`source: "nimbus-docs-host"`):

| type | payload | effect |
|---|---|---|
| `theme` | `"light"` \| `"dark"` | switches the viewer's theme |
| `ping` | — | the frame replies with `pong` |

`embedDocument` handles both directions for you. If you implement the contract
yourself, note that the viewer is served under a CSP sandbox with **no**
`allow-same-origin`, so its origin is the literal string `"null"`. Verify
messages by comparing `event.source` with your iframe's `contentWindow` —
checking `event.origin` will either reject everything or accept anyone
claiming `"null"`.

## Links expire; documents do not

A signed link defaults to 15 minutes and can be minted for 1 minute to 7 days.
When one expires the document is untouched — call `link(id)` for a fresh URL.

A viewer who is already reading is not interrupted: the signature admits them
once, and a session scoped to that document carries them for the rest of their
visit. A short link is a door, not a leash.

## Limits

- 5 MB per document.
- 50 documents per month included; test-mode keys are never metered.
- Only creations count. Views are free, so traffic to an embedded document
  never costs you.
