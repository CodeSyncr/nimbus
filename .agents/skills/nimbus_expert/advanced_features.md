# Advanced Features - Nimbus

Nimbus includes several advanced features that cater to complex application needs and performance optimization.

## Rate Limiting

-   **Multi-Store support**: Supports in-memory and Redis-backed rate limiting.
-   **Flexible Keys**: Limit by IP, User ID, or custom request attributes.
-   **Headers**: Automatically sends `X-RateLimit-*` headers to inform clients of their remaining quota.

## Static File Serving

Optimized handling for static assets with automatic compression and configurable `Cache-Control` headers.

## Health Monitoring

The `health` package provides built-in checks for:
- Database connectivity
- Cache responsiveness
- Queue status
- Custom application-level checks

## Content Security

The `shield` plugin provides comprehensive protection:
- **CSRF Protection**: Auto-injected tokens into all `.nimbus` forms.
- **Security Headers**: XSS Protection, FrameGuard, and Content-Type NoSniff.

## Service Container

Powerful dependency injection through the application's service container.
- `app.Container.Bind()`: Register a service.
- `app.Container.Make()`: Resolve a service.
- `app.Container.MustMake()`: Resolve with a panic on failure (ideal for boot).

## Best Practices

1.  **Monitor Health**: Integrate the `/health` endpoint with your load balancer or monitoring stack.
2.  **Strict Security**: Always enable the `shield` plugin in production.
3.  **Namespace Caching**: Use the `cache.Namespace` feature to avoid key collisions and enable bulk invalidation.
4.  **Lean Containers**: Only bind what you absolutely need to the service container to keep boot times fast.
