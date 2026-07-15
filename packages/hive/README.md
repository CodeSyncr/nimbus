# @codesyncr/hive

End-to-end type-safe HTTP client for [Nimbus](https://github.com/CodeSyncr/nimbus) Go applications.

Hive generates a route registry from your Go backend, then gives your TypeScript
frontend a fully-typed client — path params, request bodies, and route names are
checked at compile time.

## Installation

```bash
npm install @codesyncr/hive
```

## Generate the registry

Hive reads a route manifest produced by your Nimbus app. Run the app once with
`NIMBUS_DUMP_ROUTES=1` (it writes the manifest and exits without serving), then
generate the client:

```bash
NIMBUS_DUMP_ROUTES=1 go run .
nimbus gen:client
```

This writes `.nimbus-client/registry.ts` and `.nimbus-client/data.d.ts`.

## Usage

```ts
import { createHive } from '@codesyncr/hive'
import { registry } from './.nimbus-client/registry'

export const client = createHive({
  baseUrl: 'http://localhost:8080',
  registry,
  credentials: 'include', // for session auth
})

// Fluent, fully typed:
const post = await client.api.posts.store({ body: { title: 'Hello Hive' } })

// Path params (both /posts/:id and /posts/{id} are supported):
const one = await client.api.posts.show({ params: { id: '42' } })
```

### Zero-throw calls

`.safe()` returns a `[data, error]` tuple instead of throwing:

```ts
const [post, error] = await client.api.posts.show({ params: { id: '42' } }).safe()
if (error) {
  if (error.isStatus(404)) return notFound()
  if (error.isValidationError()) return showErrors(error.response.errors)
  return
}
```

### Retries, timeouts & cancellation

Retries fire on network errors and configured status codes — **only for
idempotent methods** — using exponential backoff with jitter, and honoring a
`Retry-After` response header.

```ts
export const client = createHive({
  baseUrl: 'http://localhost:8080',
  registry,
  timeout: 15000, // ms (default 30000)
  retry: {
    limit: 3,
    methods: ['get', 'put', 'head', 'delete', 'options', 'trace'], // default
    statusCodes: [408, 413, 429, 500, 502, 503, 504],              // default
    backoffLimit: 30000,
    onRetry: ({ attempt, delay }) => console.warn(`retry ${attempt} in ${delay}ms`),
  },
})
```

Per-call overrides:

```ts
const ac = new AbortController()
await client.api.posts.index({
  query: { tag: ['go', 'web'], page: 2 }, // arrays → ?tag=go&tag=web&page=2
  timeout: 5000,
  signal: ac.signal,
  retry: { limit: 0 },
})
```

> POST is not retried by default (non-idempotent). Add `'post'` to
> `retry.methods` only if the endpoint is safe to repeat.

### URL & route helpers

```ts
client.url('posts.show', { id: '42' }, { ref: 'docs' }) // → http://…/posts/42?ref=docs
client.has('posts.show')                                 // → true
client.current('posts.*')                                // browser-only route match
```

### Hooks

```ts
createHive({
  baseUrl, registry,
  hooks: {
    beforeRequest: [req => req.headers.set('Authorization', `Bearer ${token}`)],
    afterResponse: [(req, res) => console.log(res.status)],
    beforeError: [err => err],
  },
})
```

## License

MIT © CodeSyncr
