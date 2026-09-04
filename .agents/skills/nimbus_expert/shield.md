# Shield — request guarding

Shield is a request-inspection middleware that blocks abusive or malicious
traffic before it reaches your handlers.

## Usage

```go
app.Router.Use(shield.Guard(shield.Config{ ... }))
```

`Guard(cfgs ...Config) router.Middleware` takes an optional config; called with
no arguments it uses defaults.

## AI content guard

```go
app.Router.Use(shield.AIContentGuard(shield.Config{ ... }))
```

`AIContentGuard` applies the same machinery to model-facing input — prompt
injection and abuse patterns on the way *in* to an AI endpoint. Put it on the
routes that forward user text to a model, not globally.

## Types

| Type | Purpose |
| --- | --- |
| `Config` | What to inspect and how to respond |
| `Rule` | One matching rule |
| `BlockEvent` | Emitted when a request is blocked — the hook for logging and alerting |

Listen for `BlockEvent`s rather than reading logs: a sudden change in block rate
is usually the first sign of either an attack or a false-positive rule.

## Guidance

1. **Start in a reporting posture.** A guard tuned on synthetic traffic will
   block real users; watch `BlockEvent`s before you let it reject.
2. Shield is defence in depth, not a substitute for validation, parameterised
   queries, or authorisation. It narrows the attack surface; it does not close
   it.
3. Order matters — put Shield early in the global chain, before anything
   expensive, so blocked requests cost nothing.
4. Keep it enabled in production and disabled in tests, or fixtures that look
   adversarial will fail for confusing reasons.

Related: [middleware](middleware.md) for `SecureHeaders`, `CSRF`, and rate
limiting; [cors](cors.md) for cross-origin policy.
