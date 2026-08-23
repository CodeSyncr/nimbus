---
name: security-shield
description: Web application security best practices, OWASP Top 10 mitigations, CSRF protection, secure sessions, rate limiting, and Nimbus Shield middleware.
---

# Security Shield Expert

Application security guidelines for hardening Nimbus web apps against modern attack vectors.

## Key Defenses

1. **Input Sanitization & Parameterized Queries**:
   - Always rely on parameterized GORM/SQL statements to prevent SQL injection.
   - Validate and sanitize all incoming JSON payloads with `validation.Validate()`.

2. **Cross-Site Request Forgery (CSRF)**:
   - Use the `shield.CSRF()` middleware for state-changing browser requests (POST/PUT/DELETE).
   - Require `X-CSRF-Token` headers or hidden form fields for cookie-based authentication.

3. **Rate Limiting & Brute Force Prevention**:
   - Apply rate limiters to authentication endpoints (`/login`, `/register`, `/password/reset`).
   - Use token bucket algorithms with Redis backends in multi-instance deployments.

4. **Security Headers**:
   - Set `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and `Strict-Transport-Security`.
