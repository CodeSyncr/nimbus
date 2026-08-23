---
name: test-engineer
description: Automated testing methodologies, table-driven tests, HTTP endpoint integration tests, test databases, mocking, and browser assertions.
---

# Test Automation Engineer

Comprehensive testing standards for Go web applications and HTTP APIs in Nimbus.

## Testing Standards

1. **Table-Driven Unit Tests**:
   - Structure tests with clear test cases (`name`, `input`, `expected`, `wantErr`):
     ```go
     func TestCalculateTotal(t *testing.T) {
         tests := []struct {
             name     string
             items    []Item
             expected float64
         }{
             {"empty cart", nil, 0.0},
             {"single item", []Item{{Price: 10}}, 10.0},
         }
         for _, tt := range tests {
             t.Run(tt.name, func(t *testing.T) {
                 if got := CalculateTotal(tt.items); got != tt.expected {
                     t.Errorf("got %v, want %v", got, tt.expected)
                 }
             })
         }
     }
     ```

2. **HTTP Integration Tests with `httptest`**:
   - Use `httptest.NewRecorder()` and test request contexts to assert status codes, JSON responses, and database side effects.

3. **Isolated Test Environments**:
   - Use temporary SQLite in-memory databases or isolated schema containers for integration suites (`t.TempDir()`).
