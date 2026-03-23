# Testing - Nimbus

Nimbus provides deep support for building reliable applications through HTTP test helpers, database transaction management, and AI-assisted test generation.

## Key Features

-   **HTTP Test Helpers**: Test your routes and controllers without spinning up a full server.
-   **Database Transactions**: Automatically wrap each test in a database transaction that rolls back on completion, keeping your test database clean.
-   **Model Factories**: Effortlessly generate realistic test data using Faker-driven factories.
-   **AI Test Generation**: Scaffolds comprehensive test suites from your controller code with a single command (`nimbus test:generate`).

## Usage

### Simple HTTP Test

```go
func TestProductIndex(t *testing.T) {
    app := SetupTestApp()
    req := httptest.NewRequest("GET", "/products", nil)
    rec := httptest.NewRecorder()
    app.ServeHTTP(rec, req)
    assert(t, rec.Code, 200)
}
```

### Table-Driven Tests
The recommended pattern for covering multiple scenarios within a single test function.

## Running Tests

Use the standard Go test command:
```bash
go test ./...
```

## Best Practices

1.  **Table-Driven Tests**: Group similar test cases to improve coverage and maintainability.
2.  **Isolate Databases**: Use SQLite in-memory or a dedicated test database, combined with transactional rollbacks.
3.  **Factory Overrides**: Use model factories for 90% of your data needs, but use overrides for key scenario data.
4.  **Generate and Refine**: Use `nimbus tg` to get 80% coverage instantly, then manually add tests for complex edge cases.
5.  **Race Detection**: Frequently run with `-race` to catch concurrency issues early.
