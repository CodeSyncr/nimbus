# Captcha Plugin for Nimbus (CapSolver Alternative)

The **Nimbus Captcha Plugin** (`plugins/captcha`) is an enterprise-grade captcha plugin for the Nimbus Go framework. Powered by the **Nimbus Cloud Paid Plan**, it delivers high-speed AI captcha solving and server-side request verification.

---

## Features

- **CapSolver Alternative (Automated Solving)**: Solve Cloudflare Turnstile, Google reCAPTCHA (v2/v3/Enterprise), hCaptcha, GeeTest, AWS WAF, and Image OCR via the Nimbus Cloud API.
- **Bot Defense Middleware**: Expressive `captcha.Protect()` middleware for protecting HTTP routes and form submissions.
- **Server Verification**: Server-side verification for Turnstile, reCAPTCHA, and hCaptcha tokens.
- **Mock Mode**: Built-in mock solver and verifier for local development and unit testing without network calls or cloud costs.
- **Container Integration**: Standard Nimbus IoC bindings and facade pattern (`captcha.Solve`, `captcha.Verify`).

---

## Installation

```bash
go get github.com/CodeSyncr/nimbus/plugins/captcha
```

Register the plugin in your `bin/server.go`:

```go
package main

import (
    "github.com/CodeSyncr/nimbus"
    "github.com/CodeSyncr/nimbus/plugins/captcha"
)

func main() {
    app := nimbus.New()

    // Register Captcha plugin
    app.Use(captcha.New())

    app.Run()
}
```

---

## Environment Configuration (`.env`)

```ini
# Nimbus Cloud API Credentials (Paid Plan)
NIMBUS_CLOUD_API_KEY=nc_live_your_api_key_here

# Local Development / Testing (Auto-approves without cloud API calls)
NIMBUS_CAPTCHA_MOCK=true

# Provider Verification Secret Keys (for server-side token validation)
TURNSTILE_SECRET_KEY=0x4AAAAAA...
RECAPTCHA_SECRET_KEY=6LeIx...
HCAPTCHA_SECRET_KEY=0x000000...
```

---

## Usage Examples

### 1. Automated Captcha Solving (Scrapers / Automation)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/CodeSyncr/nimbus/plugins/captcha"
)

func solveTurnstile() {
    ctx := context.Background()

    // Solve Turnstile Challenge
    solution, err := captcha.Solve(ctx, captcha.TaskPayload{
        Type:       captcha.TaskTypeTurnstileProxyless,
        WebsiteURL: "https://example.com/login",
        WebsiteKey: "0x4AAAAAAAJn_...",
    })
    if err != nil {
        log.Fatalf("Solving failed: %v", err)
    }

    fmt.Printf("Solved Token: %s (Time: %s)\n", solution.Token, solution.SolveTime)
}
```

### 2. Protecting Routes with Middleware

Protect forms and login routes against automated bots:

```go
package routes

import (
    "github.com/CodeSyncr/nimbus/router"
    "github.com/CodeSyncr/nimbus/plugins/captcha"
)

func Register(r *router.Router) {
    // Protect /register POST route using default Turnstile token check
    r.Post("/register", captcha.Protect(), RegisterController)

    // Protect with specific options
    r.Post("/login", captcha.ProtectWithOptions(captcha.MiddlewareOptions{
        Provider:       "recaptcha",
        TokenFormField: "g-recaptcha-response",
    }), LoginController)
}
```

### 3. Server-Side Token Verification

Manual token verification in custom HTTP handlers:

```go
func VerifySubmission(c *nhttp.Context) error {
    token := c.FormValue("cf-turnstile-response")

    result, err := captcha.Verify(c.Request().Context(), "turnstile", token, c.IP())
    if err != nil || !result.Success {
        return c.JSON(400, map[string]string{"error": "Invalid captcha token"})
    }

    return c.JSON(200, map[string]string{"status": "verified"})
}
```

### 4. Image OCR (Base64)

```go
solution, err := captcha.Solve(ctx, captcha.TaskPayload{
    Type: captcha.TaskTypeImageToText,
    Body: "base64_encoded_image_string...",
})
fmt.Println("OCR Result:", solution.Text)
```

---

## Running Plugin Tests

```bash
cd plugins/captcha
go test -v ./...
```
