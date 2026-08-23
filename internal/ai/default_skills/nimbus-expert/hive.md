# Nimbus Hive — Type-Safe Client API

This agent skill documents how to generate and consume the type-safe client proxy (Hive) for frontend-backend parity.

## Code Generation Workflow

The `nimbus gen:client` CLI command walks through the backend routes registry and generates TypeScript interfaces based on schemas defined using `validation.Schema` under `.nimbus-client/registry.ts` and `.nimbus-client/data.d.ts`.

### 1. Attaching Schema to Route
Define validation schemas as standard `validation.Schema` variables. Chain the `.Schema()` method to the route:

```go
import (
    "github.com/CodeSyncr/nimbus"
    "github.com/CodeSyncr/nimbus/validation"
)

var userCreateSchema = validation.Schema{
    "name":  validation.String().Required(),
    "email": validation.String().Required().Email(),
}

func RegisterRoutes(app *nimbus.App) {
    app.Router.Post("/users", handleUserCreate).As("users.store").Schema(userCreateSchema)
}
```

### 2. Emitting Route Manifest
Dump routes registry to JSON format on server boot:
```bash
NIMBUS_DUMP_ROUTES=1 go run .
```
And generate the TypeScript files:
```bash
nimbus gen:client
```

---

## TypeScript Client Setup & Calls

### Client Instantiation
Because types are embedded directly inside the generated registry object, client setup requires zero manual type declarations or interface definitions:

```typescript
import { createHive } from '@codesyncr/hive';
import { registry } from './.nimbus-client/registry';

export const client = createHive({
  baseUrl: 'http://localhost:8080',
  registry,
  credentials: 'include',
});
```

### Monorepo Setup & Package Exports
You can expose the generated types cleanly by configuring the backend `package.json` exports mapping:

```json
{
  "name": "@my-app/backend",
  "exports": {
    "./registry": "./.nimbus-client/registry.ts",
    "./data": "./.nimbus-client/data.d.ts"
  }
}
```

This lets other packages or frontend apps in the monorepo import the registry directly:
```typescript
import { createHive } from '@codesyncr/hive';
import { registry } from '@my-app/backend/registry';

export const client = createHive({
  baseUrl: import.meta.env.VITE_API_URL || 'http://localhost:8080',
  registry,
});
```

### Call Styles

#### Fluent Proxy Style (Preferred)
```typescript
const post = await client.api.posts.store({
  body: {
    title: 'Hello Hive',
    content: 'Writing code is fun.',
    category_id: 2,
  }
});
```

#### Zero-Throw Modifier (.safe())
```typescript
const [data, error] = await client.api.posts.show({
  params: { id: '42' }
}).safe();

if (error) {
  if (error.isStatus(404)) {
    // handled type-safely
  }
  return;
}
```

### Resilience: retries, timeouts, cancellation

Configure automatic retries on `createHive`. Retries fire on network errors and configured status codes, **only for idempotent methods**, using exponential backoff with jitter capped at `backoffLimit`, and honoring a `Retry-After` response header.

```typescript
export const client = createHive({
  baseUrl: 'http://localhost:8080',
  registry,
  timeout: 15000, // per-request timeout (ms); default 30000
  retry: {
    limit: 3,
    methods: ['get', 'put', 'head', 'delete', 'options', 'trace'], // default (idempotent only)
    statusCodes: [408, 413, 429, 500, 502, 503, 504],              // default
    backoffLimit: 30000,                                            // max delay (ms)
    onRetry: ({ attempt, error, delay }) =>
      console.warn(`retry ${attempt} after ${delay}ms:`, error.message),
  },
});
```

Per-call overrides (merge over the global config) — `timeout`, `signal`, and `retry`:

```typescript
const ac = new AbortController();
const req = client.api.posts.index({
  query: { tag: ['go', 'web'], page: 2 }, // arrays → repeated params: ?tag=go&tag=web&page=2
  timeout: 5000,
  signal: ac.signal,                       // caller cancellation (combined with the timeout signal)
  retry: { limit: 0 },                     // disable retries for this call
});
// ac.abort() cancels; the request rejects/returns a `network` error.
```

**Notes / gotchas:**
- POST is **not** retried by default (non-idempotent). Add `'post'` to `retry.methods` only if the endpoint is safe to repeat.
- Array query values serialize as repeated keys (both in calls and `client.url(...)`); `null`/`undefined` values are skipped.
- A caller `signal` is combined with Hive's internal timeout signal (via `AbortSignal.any`), so it no longer clobbers the timeout.
- Client runtime lives in `packages/hive/src/`; rebuild `dist/` with `npm run build`, verify with `npm run type-check` and `npm test` (Node's built-in runner, `test/*.test.mjs`).
