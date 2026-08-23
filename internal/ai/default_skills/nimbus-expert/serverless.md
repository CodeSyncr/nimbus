# Serverless / AWS Lambda - Nimbus

Nimbus apps can run on **AWS Lambda** because the whole app is a plain
`http.Handler` (`app.Router.ServeHTTP`). The `serverless` package adapts that
handler to the Lambda proxy event model. **No AWS SDK dependency in the
framework** — the app's own `cmd/lambda` imports `aws-lambda-go`.

## Files

```
serverless/lambda.go   // Lambda(h http.Handler) Handler; event↔http.Request; Response type
cli/commands/make_lambda.go  // make:lambda generator + writeLambdaFiles() (shared with `nimbus new`)
```

## The adapter

```go
import "github.com/CodeSyncr/nimbus/serverless"

// serverless.Lambda returns func(ctx, json.RawMessage) (serverless.Response, error),
// the signature aws-lambda-go's lambda.Start accepts.
handler := serverless.Lambda(app.Router)
```

- Supports **payload v2.0** (API Gateway HTTP API, Lambda Function URLs — detected via `version:"2.0"` or `requestContext.http.method`) and **v1.0** (REST API, ALB — top-level `httpMethod`).
- Base64 request bodies (`isBase64Encoded`) are decoded; binary responses (non-UTF-8) are base64-encoded back.
- `Set-Cookie` is kept unfolded: v2 uses the dedicated `cookies` field, v1 uses `multiValueHeaders`.
- Invalid events return HTTP 400 rather than erroring the runtime.

## Generating a deploy target

```bash
nimbus make:lambda      # add to an existing app
nimbus new myapp --lambda   # or scaffold with it (also an interactive prompt)
```

Writes:
- `cmd/lambda/main.go` — calls `bin.Boot()` then `app.Boot()` (runs providers/plugins, **no HTTP listener**), then `lambda.Start(serverless.Lambda(app.Router))`.
- `template.yaml` — SAM: `AWS::Serverless::Function`, `provided.al2023`, `arm64`, `Handler: bootstrap`, `BuildMethod: makefile`, a Function URL.
- `Makefile` — `build-<FnName>:` cross-compiles `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o $(ARTIFACTS_DIR)/bootstrap ./cmd/lambda`.

`make:lambda` also runs `go get github.com/aws/aws-lambda-go@latest`; `nimbus new --lambda` leaves that to `nimbus install` (go mod tidy).

Deploy: `sam build && sam deploy --guided`.

## Serverless constraints (bake these into any Lambda app)

- **DB must go through a connection pooler** (Supabase pooler, RDS Proxy, PgBouncer) — Lambda concurrency × direct Postgres connections exhausts the server.
- **SQLite will not work** — ephemeral filesystem + CGO. Set `DB_DRIVER=postgres`.
- **`APP_ENV=production`** so `Boot()` does not run auto-migrations on every cold start; run `nimbus migrate` from CI/CD.
- **Queue workers, scheduler, WebSocket/SSE (Transmit)** need a long-running process — they don't run inside request-scoped invocations. A Lambda app is the stateless HTTP subset.

## Other targets (not built)

- **Cloudflare Workers:** feasible only as a constrained subset — Go→WASM via `syumai/workers`, no `database/sql`/GORM (no arbitrary TCP; use D1/KV/R2 bindings), WASM size limits. Not the full framework.
- **Supabase Edge Functions:** Deno/TypeScript runtime — not a Go target. Author those in TS (they can call a Nimbus origin via `@codesyncr/hive`/`@codesyncr/echo`).

Note: the `edge/` package is **origin middleware** (geo/AB/maintenance helpers), not a deployment runtime — different thing from this serverless adapter.

**Tests:** `serverless/lambda_test.go` — v2/v1 events, query strings, base64 bodies, cookies, binary responses, and a pass through the real `router.New()`. Verified end-to-end: generated `cmd/lambda` cross-compiles to a linux/arm64 `bootstrap` and returns correct proxy responses at runtime.
