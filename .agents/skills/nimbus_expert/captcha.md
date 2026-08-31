# Captcha Plugin (`plugins/captcha`) - CapSolver Alternative

The **Captcha Plugin** (`github.com/CodeSyncr/nimbus/plugins/captcha`) provides automated captcha solving (CapSolver alternative via Nimbus Cloud Paid Plan) and server-side bot verification middleware for Nimbus applications.

---

## Quickstart

```go
// bin/server.go
import "github.com/CodeSyncr/nimbus/plugins/captcha"

app := nimbus.New()
app.Use(captcha.New())
```

---

## Configuration (`.env`)

```ini
NIMBUS_CLOUD_API_KEY=nc_live_...
NIMBUS_CAPTCHA_MOCK=false
TURNSTILE_SECRET_KEY=0x4AAAAAA...
RECAPTCHA_SECRET_KEY=6LeIx...
HCAPTCHA_SECRET_KEY=0x000000...
```

---

## API & Facade Methods

### Automated Captcha Solving (`captcha.Solve`)

```go
solution, err := captcha.Solve(ctx, captcha.TaskPayload{
    Type:       captcha.TaskTypeTurnstileProxyless,
    WebsiteURL: "https://target-site.com/login",
    WebsiteKey: "0x4AAAAAA...",
})
token := solution.Token
```

Supported Task Types:
- `TaskTypeTurnstileProxyless` / `TaskTypeTurnstile`
- `TaskTypeReCaptchaV2Proxyless` / `TaskTypeReCaptchaV3Proxyless`
- `TaskTypeHCaptchaProxyless`
- `TaskTypeImageToText` (OCR vision challenges)
- `TaskTypeGeeTest`
- `TaskTypeAmazonWAF`

### Route Protection Middleware (`captcha.Protect`)

```go
// Protects HTTP routes against automated bot submissions
r.Post("/register", captcha.Protect(), RegisterHandler)
```

### Server Token Verification (`captcha.Verify`)

```go
result, err := captcha.Verify(ctx, "turnstile", token, remoteIP)
if !result.Success {
    // Handle invalid token
}
```

### Credit Check (`captcha.GetBalance`)

```go
balance, err := captcha.GetBalance(ctx)
```
