# Changelog

All notable changes to Nimbus are documented in this file.

This project follows Semantic Versioning.

## [Unreleased]

## [1.12.0] - 2026-10-04

### Security

- **`auth`: single-use tokens.** Password-reset and email-verification tokens
  are consumed atomically on first use. `auth.NewRedisTokenStore` shares them
  across instances; `WithTokenStore` sets one on the broker and verifier.
  Logout invalidates the whole session.
- **`auth`: stricter JWTs.** Only HS256 is accepted, every token must carry an
  expiry, and `auth.JWTOptions{Issuer, Audience}` binds and checks `iss` and
  `aud`. PASETO `iat` is now a number.
- **`session`: revocable cookie sessions.** Cookie sessions keep a server-side
  fingerprint registry, so expiry, replacement and logout revoke them.
- **`middleware`: trusted client IPs.** Rate limiting keys on
  `middleware.ClientIP`, which uses `X-Forwarded-For` / `X-Real-IP` only after
  `TrustedProxies` has verified the peer and walked the proxy chain.
- **`plugins/admin`: no public panel.** The panel requires an `Authorize`
  callback that checks administrative permission, and guards its routes with
  CSRF (`CSRFSecret`, defaulting to `APP_KEY`).
- **`plugins/passport`: no double use.** Authorization-code exchange and
  refresh-token rotation run in one transaction with compare-and-swap
  consumption.
- **`nimbus new`:** generated apps get random session and token secrets and
  refuse to boot with a weak one.

### Upgrading

Four changes alter behaviour for existing apps:

1. **Admin plugin:** `Boot` fails without `Config.Authorize`. Add a callback
   that checks the user is an administrator.
2. **Token secrets:** `auth.NewJWTDriver` and `auth.NewPasetoDriver` refuse
   secrets under 32 characters or the scaffold placeholder
   (`auth.ValidateTokenSecret`). Tokens already issued by the JWT driver stay
   valid (they were HS256 with an expiry).
3. **Rate limiting behind a proxy:** without the `TrustedProxies` middleware,
   requests are keyed by the socket address, so every visitor behind a proxy
   shares one bucket. Add `TrustedProxies` with your proxy's ranges.
4. **Cookie sessions end on restart:** `session.NewCookieStore` keeps its
   registry in memory. Apps with several instances, or that must keep sessions
   across deploys, use `session.NewCookieStoreWithRegistry` with a Redis or
   database store.

## [1.11.0] - 2026-10-04

### Added

- **`nimbus deploy` ships to Nimbus Cloud by default.** With no Forge config
  for another target (or `--target nimbus`), the command packs the app's
  folder, uploads it to Nimbus Deployments with the `nimbus login` token and
  follows the build log until the app is live or the build fails. `--app`
  names the project, which is created on the first deploy. `.env*`, `.git`,
  `storage/`, `tmp/`, `node_modules/` and paths listed in `.nimbusignore` are
  never uploaded. The Fly, Railway, Render, AWS, GCP, Netlify and Docker
  targets are unchanged.

### Fixed

- **`plugins/telescope`: WebSocket upgrades behind the request watcher.** The
  recorder now forwards `Hijack` (and `Unwrap`), so WebSocket endpoints in an
  app with telescope enabled upgrade instead of failing with a 500.
- **`plugins/ai`: Anthropic timeouts.** Each provider has its own HTTP
  clients: a whole answer is bounded by `AI_TIMEOUT` (it was a fixed 120s that
  ignored the setting), and streams have no overall limit, since
  `Client.Timeout` also covers the body and cut long streams off mid-answer.
- **`nimbus new`: startup errors are reported.** The generated `main.go`
  prints `app.Run`'s error and exits 1 instead of discarding it, so a failed
  boot no longer exits 0 silently.

## [1.10.0] - 2026-10-03

### Added

- **`plugins/ai`: agent skills.** `agent.WithSkills(ai.LoadSkills(fsys, dir))`
  lists each skill (a `SKILL.md` with name and description, the format
  `nimbus ai` uses) in the system prompt and adds a `load_skill` tool that
  returns its instructions, or a file from its folder, when a request needs
  it. Load from `embed.FS` to keep single-binary deploys; `ai.NewSkill`
  builds one in code. `nimbus ai` and the SDK now share one SKILL.md parser,
  which also reads multi-line YAML descriptions.
- **`plugins/ai`: `ai.NewFake`**, a scripted provider that records every
  request (and embeds deterministically), for testing agents, tools, prompts
  and RAG without a model.
- **`plugins/ai`: MCP client.** `ai.ConnectMCP` over stdio, streamable HTTP,
  SSE or in-process; `agent.WithMCP(...)`. Prefixing, allow-lists and
  opt-in approval for destructive tools.
- **`plugins/ai`: retries for every provider.** 429, 5xx, Anthropic's 529
  and dropped connections back off with jitter and honour `Retry-After`
  (`AI_MAX_RETRIES`, default 2). Providers return `*ai.APIError`.
- **`plugins/ai`: prompt caching.** `ai.WithPromptCache()`,
  `agent.WithPromptCache()` or `AI_PROMPT_CACHE`: Anthropic cache
  breakpoints on tools, system prompt and the latest message; cached tokens
  reported in `Usage` for Anthropic, OpenAI and Gemini and priced at their
  own rates by cost tracking.
- **`plugins/ai`: reasoning.** `ai.WithReasoning(effort)` /
  `WithThinkingBudget(n)` / `agent.WithReasoning`: OpenAI reasoning_effort,
  Anthropic extended thinking, Gemini thinking budgets, Ollama `think`.
  Thinking is returned (`resp.Reasoning`) and kept, signed, across tool turns.
- **`plugins/ai`: tool approval.** `RequireApproval()` tools go to
  `agent.OnApproval`, or pause the agent with `ApprovalRequiredError` until
  `agent.Resume(ctx, ai.Approve(id), ai.Deny(id, why))`.
- **`plugins/ai`: context limits.** `agent.WithContextLimit(tokens,
  ai.SummarizeOlder())` drops or summarises the oldest turns, never splitting
  a tool call from its result.
- **`plugins/ai`: files and PDFs.** `ai.WithFiles(...)` (paths, data: URIs,
  URLs): native PDF documents on Anthropic and Gemini, extracted text
  elsewhere. `ai.LoadAttachment`, `ai.DocumentText`.
- **`plugins/ai`: multi-agent.** `agent.AsTool(name, desc)` and
  `agent.Named(...).WithHandoffs(...)`; `resp.Agent` says who answered.
- **`plugins/ai`: speech.** `ai.Transcribe` (OpenAI Whisper and
  gpt-4o-transcribe, Gemini) and `ai.Speak` (OpenAI TTS, Gemini TTS as WAV).
- **`plugins/ai`: tool choice, parallel tools, reranking.**
  `ai.WithToolChoice` on every provider; `agent.WithParallelTools()`;
  `ai.Rerank` (Cohere) and `RAG.WithReranker`.

### Fixed

- **`plugins/ai`: `Agent.Stream` asked the model twice** for the final
  answer, never saved that answer to memory, and dropped `WithImages`. It
  now streams every step on providers that stream tool calls (OpenAI,
  OpenAI-compatible) and reuses the final response elsewhere.
- **`plugins/ai`: agents sent each image twice** on every step.
- **`plugins/ai`: `NewAgent` panicked without the global plugin** even when
  given a client with `WithClient`; the client is now resolved when the
  agent runs.
- **`plugins/ai`: Gemini ignored tools** (agents on Gemini never called
  them), returned only the first part of an answer, and ignored the
  per-request model. Gemini now supports function calling (streamed too),
  structured output and thinking.
- **`plugins/ai`: Ollama ignored tools and images**; both are supported, with
  streamed tool calls and usage.
- **`plugins/ai`: Cohere agents failed after their first tool call** (tool
  calls and results were sent without ids) and ignored the per-request model.
- **`plugins/ai`: Anthropic and Gemini accepted images only as local
  paths**; `data:` URIs and URLs now work, as documented.
- **`plugins/ai`: Anthropic streams tool calls and thinking**, so agents on
  Claude stream every step.

## [1.9.1] - 2026-10-02

### Fixed

- **`queue`: the Redis driver delivered delayed and retried jobs many times
  over.** Every worker moved due delayed jobs (and expired leases) back onto
  the queue without checking whether another worker already had, so with 16
  workers almost every delayed job ran more than once. Moving and claiming
  are now one Lua script, so each job goes to one worker. Jobs are now
  delivered FIFO (they were LIFO).
- **`queue`: long jobs ran twice.** Redis and database jobs past the lease
  (60s / 120s) were handed to a second worker while still running. Workers
  now renew the lease while a job runs (`queue.LeaseExtender`), and a lease
  is recorded atomically with the claim, closing the window where a crash
  lost a job for good.
- **`queue`: a job that crashes its worker no longer loops forever.** A
  delivery lost to a dead worker counts as an attempt (Redis and database).
- **`queue`: database driver.** Retries failed with a duplicate-key error
  (they reused the job's ID); they now reuse its row. Finished jobs are
  deleted instead of piling up as `done` (old `done` rows are dropped at
  boot); there is a `(queue, status, run_at)` index; reclaiming stale jobs
  runs every quarter lease instead of on every 500ms poll.
- **`queue`: unique jobs, `WithoutOverlapping` and rate limits only held
  within one process.** Locks live in a `queue.Locker` and queue rate limits
  in Redis; `queue.Boot` picks Redis or the database to match the driver.
  `WithoutOverlapping` did nothing (each wrapper had its own mutex) and could
  not be serialized; it now takes a shared lock and travels through the
  queue. A unique job's lock is released when the job finishes.
- **`queue`: `Chain.DispatchAsync` queued an empty job** (its wrapper had no
  exported fields). Each worker now queues the chain's next job.
- **`presence`: a second tab kicked out the first, and closing one tab sent
  `presence:leave` for a user still online.** Members are tracked per
  connection; join/leave fire on a user's first and last connection.
- **`middleware.RateLimit`** never forgot a key (memory grew with every new
  IP) and sent no `Retry-After`. **`RateLimitRedis`** re-armed the window on
  every request, so a client that kept retrying stayed blocked.
- **`cache`: the memory store** only dropped expired keys when read and had
  no size limit. It now sweeps expired keys, evicts least recently used past
  100,000 entries (`CACHE_MEMORY_MAX_ENTRIES`), and `Remember` (memory and
  Redis) runs one fill per key when many requests miss at once.
- **`schedule`**: sub-second interval tasks shared one 60s lock bucket.
- **`metrics`: histograms were wrong.** `_sum` added float bit patterns as
  integers and bucket counts were cumulated twice. Label values are now
  escaped.
- **`storage`**: `LocalDriver` paths could escape the root (`../`), and a
  failed `Put` left a partial file. `S3Driver.Get` cancelled its context on
  return, cutting off unread body bytes, and `Exists` reported every error
  as "missing". Signed URLs made from a path with a leading `/`, or a base
  URL with a query string, never verified.
- **`logger`**: a file channel path without a directory panicked; log
  rotation could overwrite a backup made in the same second and deleted
  unrelated files sharing the log's prefix (`app.log` vs `application.log`).
- **`encryption`**: a 16-character key that happened to be valid base64 was
  decoded to 12 bytes and rejected instead of used as-is.
- **`notification`**: Slack and Discord webhooks had no timeout.
- **`workflow`: parallel steps raced on the run's payload** (and with saves
  of the run). Steps now get their own copy of the payload and outputs are
  merged under a per-run lock.
- **`workflow`: `Cancel` did not stop a running run**, and the run then
  overwrote the cancellation when it finished. The step's context is now
  cancelled (on another instance, within `SetPollInterval`, default 1s),
  later steps are skipped, and the run stays cancelled.
- **`workflow`: `Signal` only worked on the instance running the waiting
  step, and only once it was waiting.** Stores now keep signals
  (`SignalStore`) until the step picks them up.
- **`middleware.MemoryCSRFStore`** kept every token forever. Tokens now
  expire (`TTL`, default 2h) and at most `MaxTokens` (100,000) are kept.

### Added

- **`tracing`**: distributed tracing without the OpenTelemetry SDK. W3C
  `traceparent` propagation, spans, and an OTLP/HTTP exporter configured from
  the standard `OTEL_*` variables (wired into app boot). `middleware.Tracing()`
  (now in `nimbus new` apps) continues incoming traces, `tracing.Transport`
  propagates outbound, and queue jobs carry the dispatching request's trace
  to the worker. `logger.ForRequest` adds `trace_id`.
- **`queue`: durable batches.** With the redis/database drivers,
  `Batch.Dispatch` queues every job and returns; progress lives in a
  `BatchStore` and the worker that finishes the batch runs its callbacks,
  registered by name with `queue.RegisterBatch`. `Batch.Run` keeps the old
  in-process behaviour. `queue.Release(delay)` requeues a job without
  counting an attempt.
- **`schedule`: tasks lock across instances by default** when the queue
  driver is redis/database or `REDIS_URL` is set (`SCHEDULE_LOCK=off` opts
  out).
- **`presence`: `Config.Redis`** shares members and events between
  instances; a crashed instance's users are announced as left.
- **`websocket`: `Hub.UseRedis`** fans broadcasts out to every instance.
- **`workflow`: Redis and database stores** with run leases; `Engine.Resume`
  (run periodically by the plugin) continues runs interrupted by a restart
  or a crashed instance.
- Tests for `encryption`, `hash`, `lucid`, `events`, `health`, `logger`,
  `metrics`, `notification`, `resource`, `storage`, `presence`, `websocket`
  and the Redis queue driver (via miniredis; set `NIMBUS_TEST_REDIS_URL` to
  run the Redis tests against a real server).

### Changed

- **`queue`:** `Batch.Dispatch` no longer blocks when a real driver is
  configured, and a batch with callbacks needs `Named(...)`.
  `RedisQueueWorkload.Processing` now counts leased jobs. Redis keys for
  in-flight jobs changed; jobs leased by an older version are still
  reclaimed.
- **README:** OAuth (`plugins/passport`) and Sanctum-style token abilities
  are documented as shipped; new "Running more than one instance" section;
  removed the affiliate link that was added in the v1.1.0 release commit.

## [1.9.0] - 2026-09-29

### Added

- **`plugins/cashier`: App Store Server API.** `iap.NewAppleServerAPI` reads a
  subscription's latest signed transaction and renewal info from Apple with an
  In-App Purchase key (issuer id, key id, .p8), so a server can ask Apple
  before expiring a subscription whose notification never arrived. Answers are
  reduced through the same verifier as notifications.
  - Apple's root certificates are embedded, and receipt chains are checked for
    Apple's receipt-signing extensions.
- **`plugins/cashier`: richer store state on `contracts.IAPEntitlement`.**
  `RenewalInfo`, `PeriodType` (trial, intro, normal), `PurchasedAt`, `Revoked`,
  `BillingIssue` with `GraceExpiresAt`, Google's `Token`, `LinkedToken` and
  `Acknowledged`, and StoreKit 2's `AppAccountToken`.
- **`plugins/cashier`: canonical store notifications.** `IAPNotification` now
  carries the store's delivery `ID` (for de-duplication), `Environment`,
  `Subtype`, Google's `Token`, and the `Entitlement` Apple signs into every
  notification. `Type` covers purchased, renewed, canceled, uncanceled,
  expired, refunded, grace_period, billing_issue, product_change, paused,
  recovered and test.
- **`plugins/cashier`: Google acknowledgement.** `GoogleVerifier.Acknowledge`
  and the `contracts.IAPAcknowledger` interface (Google refunds purchases not
  acknowledged within three days); the metered verifier forwards it.
- **`plugins/cashier`: lifecycle sync.** `Lifecycle.SyncProduct`,
  `RevokeProduct` and `Emit` let a host mirror store state it already knows
  (restores, re-reads of the same purchase) without emitting duplicate events.
- **`packages/cashier-cloud` 0.2.0 (web SDK):** a native renderer for paywalls
  designed in the Cashier console (flows of screens, every block type, art,
  motion), `presentPaywall()` with hosted Stripe checkout, paywall languages
  (`localizePaywall`), answers from Feedback and Marketing Consent screens,
  paywall view and close events, experiments on `offerings()`, and webhook
  signature helpers. New error codes `checkout_not_configured` and
  `already_subscribed`.

### Changed

- **`plugins/cashier`: Google Play verification** uses the purchase token as
  the stable identity of a purchase, reads subscriptions v2, and links
  upgrades, downgrades and resubscribes to the purchase they replace.

## [1.8.2] - 2026-09-26

### Fixed

- **`http`: SSE streams survive proxies that close idle connections.**
  Cloudflare and most load balancers drop a response after about 100 seconds
  without a byte, so an agent step that thought for minutes lost its
  connection and the client saw a network error instead of the answer.
  `Context.SSEStream` now writes a `: keep-alive` comment every
  `http.SSEKeepAlive` (15 seconds by default; set it to zero to turn it off).
  EventSource and other SSE clients ignore comment lines, so nothing changes
  for the receiving code.
  - `SSEWriter` is now safe to use from several goroutines at once, since the
    keep-alive ticker writes alongside the handler.
  - Nothing is written to the response after the handler returns.

## [1.8.1] - 2026-09-26

### Added

- **`plugins/ai`: `ai.Reload()` for runtime model changes.** Rebuilds the
  global AI client from the plugin config and the current `AI_*` environment,
  so an app can switch models, providers, keys and base URLs without a
  restart (for example from an admin settings page that writes the
  environment and then calls `ai.Reload()`).
  - Requests already in flight finish on the client they started with; new
    requests use the new one.
  - If the new settings cannot build a client, `Reload` returns the error and
    the previous client stays in place.
  - Before the plugin is registered it is a no-op.
  - The container's `"ai.client"` binding now resolves to the current client
    rather than the one built at boot.
- **`plugins/ai`: Fallback model on its own account.** `AI_FALLBACK_MODEL` can
  now live on a different provider, key or gateway from the main model, set
  with `AI_FALLBACK_PROVIDER`, `AI_FALLBACK_API_KEY` and `AI_FALLBACK_BASE_URL`
  (or `AI_FALLBACK_API_URL`). A `GenerateRequest` or `StreamRequest` that names
  the fallback model is routed to that account; every other model stays on the
  main one. With none of these set, the fallback runs on the main account as
  before.
  - Added matching fields to `ai.Config`: `FallbackProvider`, `FallbackModel`,
    `FallbackAPIKey`, `FallbackBaseURL`.
  - A misconfigured fallback is reported on fallback requests only; it never
    takes the main model down.

### Fixed

- **`http`: SSE streams are no longer cut off at `SERVER_WRITE_TIMEOUT`.**
  `Context.SSEStream` now lifts the server's read and write deadlines for its
  own response (via `http.ResponseController`), so long agent replies that
  run tools for minutes finish instead of dropping mid-answer with a
  connection reset. The stream still ends when the request context does, and
  other responses keep the server's timeouts.
- **`plugins/ai`: Base URLs that include `/chat/completions`.** Image, video
  and fallback base URLs are normalised by dropping a trailing slash and a
  trailing `/chat/completions`, which the clients append themselves. A URL
  copied from a gateway's docs no longer produces
  `…/chat/completions/chat/completions`.

## [1.8.0] - 2026-09-25

### Added

- **`plugins/ai`: Independent Image & Video Provider Configuration.** Image and
  video generation requests can now be served by separate dedicated providers,
  keys, and endpoints from the primary text generation model.
  - New environment variables: `AI_IMAGE_PROVIDER`, `AI_IMAGE_MODEL`,
    `AI_IMAGE_API_KEY`, `AI_IMAGE_BASE_URL` (or `AI_IMAGE_API_URL`),
    `AI_VIDEO_PROVIDER`, `AI_VIDEO_MODEL`, `AI_VIDEO_API_KEY`,
    `AI_VIDEO_BASE_URL` (or `AI_VIDEO_API_URL`).
  - Added matching fields to `ai.Config`: `ImageAPIKey`, `ImageBaseURL`,
    `VideoProvider`, `VideoModel`, `VideoAPIKey`, `VideoBaseURL`.
  - When left unset, image and video generation gracefully fall back to the text
    provider and model configuration.
- **`plugins/ai`: Enhanced Tool JSON Schema Reflection.**
  - Struct fields tagged with `omitempty` in their `json` tag are no longer
    marked as `required` in the generated JSON Schema for tool calls, allowing
    models to accurately recognize optional parameters.
  - Added typed element schema reflection (`itemSchema`) for slices and arrays,
    enabling models to understand the nested item types of array arguments.

### Fixed

- **`plugins/ai`: Resilient Retry for Dropped Connections.**
  - Added automatic detection and retry (up to 5 attempts with backoff) for
    requests where the server or gateway terminates the connection before
    responding (unexpected EOF, connection resets, broken pipes, idle timeouts,
    or HTTP/2 GOAWAY frames), which frequently occurs on heavily loaded providers
    such as DeepSeek.
  - Clears idle connections from the HTTP transport pool between retry attempts
    to prevent subsequent attempts from reusing dead sockets.

## [1.7.0] - 2026-09-11

### Added

- **`plugins/ai`: OpenAI Vision Support.** Multi-modal prompt inputs and image
  attachments are now supported natively by the OpenAI provider.
- **`nimbus expose` & Nimbus Tunnel Relay (`plugins/tunnel`):**
  - First-class local tunnel command (`nimbus expose <port>`) connecting local
    development servers to public tunnel endpoints (`tunnel.nimbusgo.space`).
  - Support for reserved subdomains that persist across reconnects, two-character
    subdomains, and secure parent-domain cookie isolation.
- **Document Plugin (`plugins/docs`):**
  - Added Document API clients and plugin system for dynamic documentation
    indexing and programmatic access.

## [1.6.1] - 2026-09-08

### Breaking

- **`plugins/cashier`: the Cashier Cloud key gate is gone, and three exported
  identifiers went with it.** The subscription and in-app-purchase suite is
  now always available, so nothing has to be unlocked before it works. If you
  reference any of the following, your build will fail on upgrade:
  - `cashier.ErrCloudRequired` — removed. Nothing returns it any more.
  - `(*Cashier).CloudEnabled()` — removed. The suite is always enabled; delete
    the check.
  - `Config.CloudKey` and the `CASHIER_CLOUD_KEY` environment variable —
    removed. Drop them from your config and environment; they are ignored.
  - The plugin's info output no longer carries `cloud_enabled`.

  This is a behaviour change in a patch release because it only ever *adds*
  capability: code that previously hit `ErrCloudRequired` now works. The
  removals above are the whole of the breakage.

### Fixed

- **`plugins/telescope`: streaming responses were buffered until the handler
  returned.** The request watcher wraps the response writer to record status
  and body, but its recorder did not implement `http.Flusher` — so it hid the
  underlying writer's, and any Server-Sent Events or chunked response behind
  the watcher was withheld until the request finished. Long-running streams
  appeared to hang and then arrive all at once. The recorder now forwards
  `Flush`, with a compile-time assertion so it cannot regress.
- **`plugins/ai`: long generations were cut off after 60 seconds.** The
  client timeout covers reading the entire response body, so a long streamed
  answer or a multi-step tool run was killed mid-flight rather than merely
  being slow to start. The default is now 600s.
- **`plugins/ai`: the Gemini provider ignored the configured timeout**, using
  a fixed 120s client regardless of `Config.Timeout`. It now honours the
  configuration like every other provider.

### Added

- **`AI_TIMEOUT`** (seconds) overrides the AI client timeout, for deployments
  whose model or agent runs need more or less headroom than the default.

## [1.6.0] - 2026-09-04

### Added
- **Cashier Subscriptions & In-App Purchases (`plugins/cashier`):**
  - Full subscription lifecycle support across Stripe (`gateways/stripe_subscriptions`) and Razorpay (`gateways/razorpay_subscriptions`).
  - In-App Purchase (IAP) validation engine for Apple App Store and Google Play (`plugins/cashier/iap`).
  - Customer info management, catalog management, product entitlements, refund handling, and Cloud Meter integration.
- **Captcha Plugin (`plugins/captcha`):**
  - Modular Captcha plugin with client verification, background solver server, and Turnstile/reCAPTCHA solver integration.
- **Gemini Image Generation (`plugins/ai`):**
  - First-class Gemini Image Generation provider integration (`gemini_image.go`).

### Changed
- **`nimbus ai` now investigates before it acts.** Every request runs an explore → plan → execute → verify loop instead of generating files blind:
  - *Explore:* the agent reads the codebase with read-only tools (`find_files`, `grep`, `read_file` with line ranges, `list_dir` with depth) and writes a findings report before any plan is made.
  - *Plan:* the plan is grounded in those findings; questions are answered directly instead of forcing a plan.
  - *Execute:* the model reads files before editing them, uses targeted `edit_file` changes (CRLF-tolerant, `replace_all`), and runs the build.
  - *Verify:* after execution `go build ./...` / `go vet ./...` run automatically and failures are fed back for up to three repair rounds.
- **Conversation memory.** Sessions remember each request, plan, outcome and the files it changed; follow-up prompts see that history, and `--resume` restores it.
- **Project instructions.** `AGENTS.md`, `NIMBUS.md`, `CLAUDE.md` and `.nimbus/instructions.md` are loaded into the agent's context as persistent project guidance.
- **History compaction no longer blinds the model.** Tool output sent back to the model was being cut to 100 characters (so `read_file` and build errors were unreadable); recent results are now kept in full and only older ones are elided.
- New `/api/v1/ai/turn` client protocol (single agentic model turn with native tools); older servers fall back to the previous `/ai/plan` + `/ai/execute` flow automatically.
- `bash` tool works on stock Windows (falls back to `cmd /C` when `sh` is unavailable), captures head+tail of long output, and has a 120s timeout.

- **`nimbus ai` TUI redesigned** in the style of Claude Code: one-line header with project/branch/mode, Claude-style tool activity lines (`● Read main.go  42 lines`, `● Edit start/routes.go  +2 −0` with inline diffs), phase markers with timings (Exploring → Planning → Executing → Verifying), streamed assistant text, a plan-review card that scrolls, restyled clarification questions, adaptive light/dark palette, `Esc` to interrupt a running task, `/session` and `/help` commands, and prompts passed on the command line (`nimbus ai "add a comments resource"`) now run immediately.

### Fixed
- **`nimbus ai` on Windows:** the console is switched to UTF-8 and virtual-terminal mode before the TUI starts, so glyphs and colours render in cmd.exe/conhost instead of mojibake.
- **Tests overwrote the real `~/.nimbus/auth.json` on Windows.** The CLI tests set `HOME` to a temp dir, but Go resolves the home directory from `USERPROFILE` on Windows, so running the test suite replaced the developer's login with mock credentials pointing at a dead localhost server — after which `nimbus ai` could not reach Nimbus Cloud. Added `NIMBUS_CONFIG_DIR` (honoured by `auth.ConfigDir`) and pointed every test at it. If you ran the tests before this fix, run `nimbus login` again.

### Added
- **`plugins/ai`: native tool calling for the OpenAI provider** (function tools, `tool_calls` parsing, `role: tool` results, streamed tool-call accumulation). The Anthropic provider now sends proper `tool_use` / `tool_result` blocks instead of flattening them to text.

### Fixed
- **`nimbus serve` on Windows:** Air runs the built binary through `cmd /c`, which refuses extension-less files, so `./tmp/main` failed with `'...\tmp\main' is not recognized as an internal or external command`. Generated `.air.toml` now uses `./tmp/main.exe` on Windows, and `serve` patches existing configs in place before launching Air.
- **`nimbus serve` shutdown on Windows:** Ctrl+C now kills the whole Air/app process tree instead of only the top-level `go run` wrapper, so the dev port is no longer left bound by an orphaned app.

## [1.5.3] - 2026-08-23

### Added
- **Agentic AI Workspace Engine (`internal/ai` & `internal/ai/tui`):**
  - Full-featured Bubbletea TUI with markdown rendering, interactive plan visualization, question handling, diff previews, and tool execution.
  - Built-in embedded skills system (`internal/ai/default_skills`) containing 20+ expert skills (`nimbus-expert`, `go-architect`, `database-migrations`, `livewire-components`, `mcp-builder`, `test-engineer`, `frontend-design`, etc.).
  - Multi-turn session management, automated tool orchestration, and agent reasoning loop.
- **Custom AI Base URLs & Resiliency:**
  - Added support for custom provider API base URLs (`OPENAI_BASE_URL` / `OPENAI_API_URL` and `ANTHROPIC_BASE_URL` / `ANTHROPIC_API_URL`).
  - Added exponential backoff retry handler (3 attempts) in OpenAI provider for transient 502/503/504/429/gateway/timeout errors.

## [1.5.2] - 2026-08-20

### Added
- **Claude Code CLI-Style Interactive AI Workspace (`nimbus ai`):**
  - Interactive multi-turn AI terminal REPL session with session banner, project detection, and prompt loop.
  - Interactive slash commands: `/help`, `/clear`, `/context`, `/models`, `/routes`, `/offline`, and `/exit`.
  - Immediate interactive drop-in when running with or without an initial prompt.
  - Claude Code style `[+]` file generation indicators and post-generation architectural hints.

### Fixed
- **Shield CSRF Wildcard Path Matching:**
  - Fixed `isExceptPath()` in `packages/shield` to properly strip trailing asterisks (`*`) in wildcard exclusion patterns (e.g. `/api/*`, `/api/v1/*`), preventing 403 CSRF rejections on API endpoints.
- **Strict Error Handling in `nimbus ai`:**
  - Removed silent fallback to local file generator on Cloud AI failures; exact server and connection errors are now cleanly surfaced to the developer.

## [1.5.1] - 2026-08-20

### Fixed
- **Database Schema Builder Dialect Normalization:**
  - Automatically normalize boolean column default values (`"0"`, `"1"`, `"false"`, `"true"`) to `FALSE`/`TRUE` on PostgreSQL and `0`/`1` on MySQL and SQLite.
  - Fixed SQL error `42804` (boolean column with integer default expression) during PostgreSQL / Supabase migrations.

## [1.5.0] - 2026-08-19

### Added
- **Nimbus Cloud & CLI Authentication Subsystem:**
  - Added `nimbus login`, `nimbus logout`, and `nimbus whoami` CLI commands for browser-based OAuth authentication with Nimbus Cloud (`https://nimbusgo.space`).
  - Added secure token management and profile caching in `~/.nimbus/auth.json`.
  - Added redesigned, light-themed terminal authorization callback page with clean typography and real-time session indicators.
- **AI Copilot Cloud Synthesizer (`nimbus ai`):**
  - Integrated `nimbus ai "<prompt>"` cloud code generation engine connected to Nimbus Cloud AI services.
  - Added support for subscription tier gating and seamless fallback to `--offline` rule-based code generation.
  - Added AI chat context with strict Nimbus Go framework CLI ergonomics (`nimbus serve`, `nimbus make:*`, `nimbus db:migrate`).

## [1.4.0] - 2026-08-19

### Added
- **Warmup Lifecycle Phase & Application Modes (`nimbus.AppMode`, `nimbus.AppState`):**
  - Added `app.WarmUp()` allowing deterministic assembly and inspection without starting HTTP listeners, queue consumers, or background schedulers.
  - Added first-class application modes: `ModeRun` (default), `ModeWarmup`, `ModeTest`, and `ModeCli`.
  - Added lifecycle events `events.AppWarmed` and `events.AppReady`.
  - Thread-safe lifecycle state and mode management guarded by `sync.RWMutex`.
- **Functional Constructor Options:**
  - `nimbus.New(opts ...Option)` with options `WithMode`, `WithPort`, and `WithConfig` while maintaining 100% backward compatibility for `nimbus.New()`.
- **Direct `net/http.Handler` Implementation:**
  - `*App` now directly implements `ServeHTTP(w, r)`, allowing direct usage in `httptest.NewServer(app)` and custom HTTP handler composition.
- **Extended Provider Lifecycle Hooks:**
  - Added optional `HasStart` and `HasShutdown` hooks for service providers (skipped during warmup).
- **First-Party Context Ergonomics & Encapsulation:**
  - Added `ctx.BindQuery(&dest)` and `ctx.BindForm(&dest)` to `*nhttp.Context`.
  - Added `ctx.SaveUploadedFile(file, dst)` to simplify saving multipart files to disk.
  - Added `ctx.ValidationErrors(errors)` for standard 422 Unprocessable Entity responses.
  - Added `ctx.SSEStream(fn func(w *SSEWriter) error)` with `SSEWriter.Event(event, data)`.
- **Route Manifest & OpenAPI Tooling:**
  - Added `app.DumpRoutes(outDir ...string) error` for programmatic client codegen during warmup.
  - Added `app.DumpOpenAPI(outPath ...string) error` for programmatic OpenAPI 3.0 specification generation.
- **Testing Subsystem (`nimbus/testing`):**
  - Added `testing.New(app *nimbus.App)` with automatic warmup in `ModeTest`.
  - Enhanced fluent assertion chaining (`AssertOK`, `AssertCreated`, `AssertStatus`, `AssertJSONPath`, `AssertHeader`, `AssertContains`).

## [1.3.0] - 2026-07-17

### Added
- **Serverless / AWS Lambda deployment.** A new `serverless` package adapts any
  `http.Handler` (e.g. `app.Router`) to the AWS Lambda proxy event model — no
  AWS SDK dependency in the framework. Supports API Gateway / Function URL
  payload v2.0 and v1.0 (REST API / ALB), including base64 request/response
  bodies and correct `Set-Cookie` handling.
  - `nimbus make:lambda` scaffolds a Lambda deployment target into any app:
    `cmd/lambda/main.go` (serves the router via the adapter), a SAM
    `template.yaml` (Function URL, `provided.al2023`, arm64), and a Makefile
    build rule.
  - `nimbus new --lambda` (and an interactive prompt) adds the same target when
    scaffolding a new app.
  - Docs note the serverless constraints: use a DB connection pooler, SQLite
    won't work (ephemeral FS + CGO), and queue/scheduler/WebSocket need a
    long-running process. Cloudflare Workers (Go→WASM subset) and Supabase Edge
    Functions (Deno/TypeScript) are documented as out of scope for the Go app.

### Client packages (npm)
- **`@codesyncr/echo`** (real-time SSE client for Transmit): **fixed** a bug
  where `stopListening()` was a no-op — a removed callback kept firing because
  the listener lived in two registries and only one was cleared. Added a 25-test
  suite (previously untested). Published as `@codesyncr/echo@1.0.1`.
- **Renamed and published to npm**: `@codesyncr/nimbus-hive` → **`@codesyncr/hive`**
  and `@nimbus/echo` → **`@codesyncr/echo`**. Added a Hive README and a repo
  `LICENSE` (MIT was declared but no license file existed — this also fixes
  license detection for the Go module on pkg.go.dev).

## [1.2.0] - 2026-07-15

### Added
- **Cashier Plugin (`plugins/cashier`):** Multi-gateway billing, modeled on Laravel Cashier but gateway-agnostic.
  - Gateways: **Stripe**, **Razorpay**, and **PayU**, each with real webhook signature verification (Stripe/Razorpay HMAC-SHA256, PayU SHA-512).
  - `GatewayManager` for registering several gateways in one app, selecting a default (`Config.Default` → `PAYMENTS_DEFAULT_GATEWAY` → first registered), and routing a charge per request.
  - Paywall with `RequirePlan` middleware (HTTP 402), pluggable `EntitlementStore`, and canonical cross-gateway events via `events.Normalize`.
  - `FromEnv` auto-registers any gateway whose credentials are present. Ships `cashier_transactions` / `cashier_subscriptions` migrations.
- **Passport Plugin (`plugins/passport`):** OAuth2 authorization server, modeled on Laravel Passport.
  - Grants: `authorization_code` (with **PKCE**, S256/plain), `client_credentials`, and `refresh_token` (with rotation — the old token pair is revoked).
  - RFC 7662 token introspection and RFC 7009 revocation. Opaque tokens stored SHA-256 hashed, so they are fully revocable.
  - Confidential and public clients (PKCE enforced for public clients by default, per OAuth 2.1), per-client redirect/scope allowlists, first-party consent skip, and a bundled consent screen.
  - Resource-server middleware: `RequireAccessToken` (401 + `WWW-Authenticate`) and `RequireScope` (403).
- **Admin Plugin (`plugins/admin`):** Resource-based CRUD admin panel, modeled on Nova/Filament.
  - Reflection-driven over any `database.Model` — declares no migrations of its own.
  - Field constructors (`Text`, `Textarea`, `Number`, `Boolean`, `Email`, `Password`, `Date`, `Select`) with chainable modifiers (`WithLabel`, `AsSortable`, `AsReadonly`, `HideFromIndex`, `HideFromForm`).
  - Zero-config field inference from struct types when `Fields` is omitted; paginated list, create/edit/delete screens; gated by `Config.Middleware`.
- **Browser Testing (`testing/browser`):** Dusk-style end-to-end harness driving the app in-process through its `http.Handler`.
  - Cookie jar, redirect following, link clicking, and form submission that automatically carries hidden inputs (e.g. CSRF).
  - Fluent assertions: `AssertSee`, `AssertDontSee`, `AssertSeeIn`, `AssertPathIs`, `AssertQueryStringHas`, `AssertTitle`, `AssertInputValue`, `AssertStatus`/`AssertOk`, `AssertHeader`.
  - Pure stdlib — no browser binary or new dependencies required.
- **Auth:** Token ability (scope) checks matching Laravel Sanctum's `ability:`/`abilities:` semantics — `HasAnyAbility` / `HasAllAbilities` on `PersonalAccessToken`, plus `RequireAnyAbility` (OR) and `RequireAllAbilities` (AND) middleware.
- **CLI:** `nimbus key:generate` for generating `APP_KEY`, wired into `nimbus new` scaffolding.
- **AI Plugin:** Implemented the **Anthropic**, **Mistral**, and **Cohere** providers.
- **Config:** Startup configuration validation (`Config.Validate`) with actionable errors, and configurable HTTP server timeouts.
- **Router:** `router.Manifest` / `router.WriteManifest` expose registered routes for code generation, plus `router.PathParams` and `router.DeriveName`.

### Fixed
- **`nimbus gen:client` produced an empty registry for every project.** Three compounding bugs:
  - `WriteRouteManifest` was never called by the framework, so the documented `NIMBUS_DUMP_ROUTES=1` flow wrote nothing and generation always fell back to an empty skeleton. The app now writes the manifest at startup when `NIMBUS_DUMP_ROUTES=1` (honoring `NIMBUS_CLIENT_OUT`) and exits without serving.
  - Routes registered without an explicit `.As(...)` name were silently skipped. Unnamed routes now get a stable, REST-conventional derived name (`GET /api/posts/{id}` → `api.posts.show`), guaranteed unique; explicit `.As(...)` still wins.
  - Only `:id` path params were recognized. Both `:id` and `{id}` syntaxes are now supported.
  - Generation no longer writes an empty file silently — it fails with instructions when no manifest exists.
- **Errors:** Default HTML error pages, and zero-config 404s now render correctly.
- **Middleware:** Structured request logging.

### Hive TypeScript client (`@codesyncr/hive`)
- **Added:** Automatic retries — configurable `limit`, `methods` (idempotent-only by default), `statusCodes`, exponential backoff with jitter capped at `backoffLimit`, `Retry-After` header support, and an `onRetry` hook. Previously `retry` existed in the config type but was **never implemented**.
- **Added:** Per-request `timeout`, `signal`, and `retry` overrides.
- **Fixed:** A caller-supplied `AbortSignal` was overwritten by the internal timeout signal; the two are now combined.
- **Fixed:** Array query values serialized as `?tag=a%2Cb` instead of repeated `?tag=a&tag=b` params.
- **Fixed:** Path params now support both `{id}` and `:id`, are URL-encoded, and `:id` no longer matches inside `:idx`.

### Chore
- Added a `.gitignore` and removed accidentally committed artifacts from tracking, including a 28 MB compiled test binary (`livewire.test`) that had shipped in every release since v0.1.8, and stray `.DS_Store` files.

## [1.1.0] - 2026-05-21

### Added
- **Supabase Plugin:** Added first-class integration with Supabase services (`plugins/supabase`), including:
  - Auth client (`GoTrue`) for signing up, signing in, and managing user sessions.
  - Database client (`PostgREST`) for calling database RPC functions.
  - Realtime client for subscribing to channels and listening for database change events.
  - Verification middleware (`VerifySupabaseJWT`) to authenticate incoming API requests.
- **Template Engine:** Added support for Nested Components (dot notation subdirectory mapping, e.g. `@field.root(...)`).
- **Template Engine:** Finalized the Props and Provide/Inject Context APIs in the `.nimbus` rendering engine. Added lazy slot rendering to resolve parent-child rendering evaluation order.

### Fixed
- **Template Engine:** Changed the return type of `$props.toAttrs()` to `template.HTMLAttr`, bypassing Go's default context-aware auto-escaping and resolving `ZgotmplZ` errors.

## [1.0.1] - 2026-05-08

### Fixed
- **Security:** Resolved SQL injection vulnerability in Tenancy schema scoping.
- **Security:** Renamed `EncryptDeterministic` to `EncryptDeterministicUNSAFE` to highlight cryptographic risks.
- **Security:** Fixed WebSocket origin checker incorrectly rejecting all connections by default.
- **Concurrency:** Fixed data races and TOCTOU bugs in `logger` channels, `ai` provider registry, `presence` channels, and `cache` locks.
- **Middleware:** Implemented full logic for `RequireVerifiedEmail` and fixed HTTP spec violations in `ratelimit_redis` (correctly handles `Retry-After`).
- **Optimization:** Moved regex compilation in `shield` out of the hot path.

## [1.0.0] - 2026-03-23

First **stable** release (`v1.0.0`). The packages listed under **Versioning & stability** in `README.md` follow SemVer: breaking changes require a new major version after deprecation when possible.

### Added
- **CLI:** `nimbus plugin install` and `nimbus plugin list` as nested commands (same behavior as `plugin:install` / `plugin:list`).
- **Tests:** coverage for `router` (named URLs, groups, route metadata), `http` context helpers, `session` middleware, `database` migrator (`Fresh` on SQLite, `dropTableSQL`).

### Changed
- **`database.Migrator.Fresh`:** dialect-safe `DROP TABLE` (PostgreSQL uses `CASCADE`; SQLite/MySQL no longer use invalid `CASCADE` on SQLite).

### Previously unreleased (rolled into 1.0.0)

#### Added
- Queue reliability hardening:
  - retry backoff with jitter
  - Redis in-flight processing + visibility timeout reclaim
  - database queue lease reclaim and completion support
- Realtime security hardening:
  - websocket and presence origin allowlist support with safe same-origin default
- Queue telemetry counters:
  - retried and reclaimed signals in Horizon stats
  - Prometheus-style Horizon metrics endpoint
- Migration safety improvements:
  - transactional migration execution on supported dialects
  - per-migration `NonTransactional` override
- CI baseline workflow with:
  - `go test ./...`
  - `go test -race ./...`
  - `go vet ./...`

#### Changed
- Public docs expanded:
  - getting started path
  - production readiness checklist
  - versioning/release policy
  - release checklist (`V1_RELEASE.md`)

#### Fixed
- `/docs/getting-started` docs page registration and routing.
- `App.Run` / `RunTLS`: always cancel scheduler context on exit (including `Serve` errors) to satisfy `go vet` and avoid leaks.

### Known limitations (v1)

- **Telescope** plugin: many panels remain preview / “coming soon”; not treated as a v1-stable surface—see `README.md`.
- **First-party OAuth / API tokens** (Sanctum/Passport-class): not included in v1; document your own token strategy or wait for a future release.
- **HTML error pages** (404/500): applications should register `router.Fallback` and custom handlers; core focuses on structured JSON/API errors.
- **Locale:** v1 supports programmatic `locale.AddTranslations` / middleware; file-based translation loading is not the primary focus.

---

## Earlier history

Prior development was not consistently tagged in this changelog; see git history for detail.
