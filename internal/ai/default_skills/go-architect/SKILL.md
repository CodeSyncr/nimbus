---
name: go-architect
description: Senior Go engineering patterns, concurrency, channel pipelines, clean architecture, error handling, interfaces, benchmarking, and profiling.
---

# Go Backend Architect

Architectural guide for writing idiomatic, high-performance, maintainable Go code.

## Core Guidelines

1. **Explicit Error Handling**:
   - Always wrap errors with context: `fmt.Errorf("failed to load user %d: %w", id, err)`.
   - Never ignore returned errors; handle them at the boundary or pass them up.

2. **Clean Interfaces & Decoupling**:
   - Accept interfaces, return concrete structs.
   - Keep interfaces small (1-3 methods) defined at the consumer side.

3. **Safe Concurrency & Channel Pipelines**:
   - Always manage goroutine lifecycles using `context.Context` cancellation.
   - Prevent goroutine leaks by ensuring channel readers/writers terminate cleanly on `ctx.Done()`.

4. **Performance & Memory Efficiency**:
   - Minimize heap allocations in critical hot paths (`sync.Pool`, pre-allocated slices with `make([]T, 0, cap)`).
   - Use `strings.Builder` or `bytes.Buffer` for dynamic string concatenations.
