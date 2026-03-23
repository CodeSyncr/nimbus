# Changelog

All notable changes to Nimbus are documented in this file.

This project follows Semantic Versioning.

## [Unreleased]

### Added
- Queue reliability hardening:
  - retry backoff with jitter
  - Redis in-flight processing + visibility timeout reclaim
  - database queue lease reclaim and completion support
- Realtime security hardening:
  - websocket and presence origin allowlist support with safe same-origin default
- Queue telemetry counters:
  - retried and reclaimed signals in Horizon stats
  - Prometheus-style Horizon metrics endpoint
- Migration safety improvements:
  - transactional migration execution on supported dialects
  - per-migration `NonTransactional` override
- CI baseline workflow with:
  - `go test ./...`
  - `go test -race ./...`
  - `go vet ./...`

### Changed
- Public docs expanded:
  - getting started path
  - production readiness checklist
  - versioning/release policy
  - release checklist

### Fixed
- `/docs/getting-started` docs page registration and routing.
