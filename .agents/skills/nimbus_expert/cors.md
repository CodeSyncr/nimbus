# CORS & Security Headers - Nimbus

Nimbus provides built-in middleware for handling Cross-Origin Resource Sharing (CORS) and other security-related headers.

## CORS Middleware

The CORS middleware allows you to configure which origins, methods, and headers are allowed for cross-origin requests.

### Usage

```go
import "github.com/CodeSyncr/nimbus/middleware"

app.Router.Use(middleware.CORS("https://example.com"))
```

### functionality

-   `Access-Control-Allow-Origin`: Sets the allowed origin.
-   `Access-Control-Allow-Methods`: Defaults to `GET, POST, PUT, PATCH, DELETE, OPTIONS`.
-   `Access-Control-Allow-Headers`: Defaults to `Content-Type, Authorization`.
-   **Preflight Requests**: Automatically handles `OPTIONS` requests and returns `204 No Content`.

## Security Headers

Additional middleware is available for setting security headers:

-   **SecureHeaders**: Sets headers like `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, and `Content-Security-Policy`.
-   **Gzip**: Enables transparent Gzip compression for responses.
-   **BodyLimit**: Limits the size of the request body to prevent Denial of Service (DoS) attacks.

## CSRF Protection

The `middleware.CSRF` provides protection against Cross-Site Request Forgery.

### Usage

1.  Initialize a `CSRFStore` (e.g., `NewMemoryCSRFStore`).
2.  Add the middleware: `app.Router.Use(middleware.CSRF(store))`.
3.  Include the token in forms (`csrf_token` field) or headers (`X-CSRF-Token`).

## Rate Limiting

Nimbus supports both in-memory and Redis-backed rate limiting.

### In-Memory
```go
app.Router.Use(middleware.RateLimit(100, time.Minute, func(r *http.Request) string {
    return r.RemoteAddr // Limit by IP
}))
```

### Redis-Backed
Requires the Redis plugin.
```go
app.Router.Use(middleware.RateLimitRedis(rdb, 1000, time.Hour))
```
