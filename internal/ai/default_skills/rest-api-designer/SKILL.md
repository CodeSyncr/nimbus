---
name: rest-api-designer
description: Production RESTful API patterns, JSON response envelopes, HTTP status codes, OpenAPI schemas, pagination, rate limiting, and authentication.
---

# REST API Designer

Standardized patterns for designing robust, intuitive, and secure RESTful HTTP APIs.

## Conventions

1. **Resource URIs & HTTP Verbs**:
   - `GET /api/v1/resources` (List, supports `?page=1&limit=20&sort=-created_at`)
   - `POST /api/v1/resources` (Create, returns `201 Created` with resource body)
   - `GET /api/v1/resources/{id}` (Retrieve, returns `200 OK` or `404 Not Found`)
   - `PUT /api/v1/resources/{id}` (Full replace)
   - `PATCH /api/v1/resources/{id}` (Partial update)
   - `DELETE /api/v1/resources/{id}` (Delete, returns `204 No Content`)

2. **Standard JSON Response Envelopes**:
   - Success list response:
     ```json
     {
       "data": [...],
       "meta": { "page": 1, "per_page": 20, "total": 142 }
     }
     ```
   - Error response:
     ```json
     {
       "error": {
         "code": "VALIDATION_FAILED",
         "message": "The email field is required.",
         "details": { "email": ["Must be a valid email address"] }
       }
     }
     ```

3. **Authentication**:
   - Bearer tokens (JWT / Paseto) passed via `Authorization: Bearer <token>`.
