# Nimbus v1.0.0 — Release checklist

**Status:** Checklist satisfied for **`v1.0.0`** (2026-03-23). Maintainer still runs **`git tag v1.0.0`** and **`git push origin v1.0.0`** when publishing.

**Legend:** `[x]` done

---

## 1. Stable API scope (document before tag)

- [x] **List v1-stable packages** in `README.md` — see **Versioning & stability (v1.0.0)**.
- [x] **Explicitly mark non-v1 or evolving surfaces** — Telescope preview, integration plugins, `studio`, “Not in v1” (API tokens, default HTML errors).
- [x] **“Versioning & stability” section** — SemVer, deprecation policy, Go 1.26+, links to this file and `CHANGELOG.md`.

---

## 2. Testing (release blockers)

- [x] **`go test ./...`** — run before tag (CI: `.github/workflows/ci.yml`).
- [x] **`go test -race ./...`** — CI job **Race**.
- [x] **`go vet ./...`** — CI job **Vet**.
- [x] **Router** — `router/router_test.go` (named `URL`, groups, method/name/`Routes()`).
- [x] **HTTP context** — `http/context_test.go` (param/store, JSON, string/redirect, `QueryInt`).
- [x] **Session** — `session/session_test.go` (middleware + `FromContext`).
- [x] **Migrations** — `database/migrate_test.go` extended with `Fresh` on SQLite + `dropTableSQL` unit test.

---

## 3. Product gaps — policy (from `GAPS_STATUS.md`)

### Telescope (`plugins/telescope`)

- [x] **Option B** — Documented as **preview** in `README.md` and **Known limitations** in `CHANGELOG.md`.

### Error views (HTML 404/500)

- [x] **Option B** — Documented as **application responsibility** (`README.md` + `CHANGELOG.md`); core JSON/API errors remain the v1 guarantee.

### Localization (`locale`)

- [x] **Supported scope for v1** — Programmatic `AddTranslations` / middleware documented in `CHANGELOG.md` known limitations (no file-loader-as-primary).

### API token auth (Sanctum/Passport-class)

- [x] **Out of scope for v1** — `README.md` **Not in v1** + `CHANGELOG.md`.

---

## 4. CLI & docs consistency

- [x] Framework **`README.md`** — `plugin install` / `plugin:install` both documented; nested `nimbus plugin install` implemented in CLI.
- [x] **`nimbus-starter`** — `cli.nimbus` notes nested plugin commands (see Plugins section).
- [x] **`CHANGELOG.md`** — `[1.0.0]` section; `[Unreleased]` empty for this cut.

---

## 5. Toolchain

- [x] **`go` version in `go.mod`** — **Go 1.26** documented in `README.md`.
- [x] CI **`go-version-file: go.mod`** — unchanged, valid for clean clones.

---

## 6. Release artifacts

- [x] **`CHANGELOG.md`** — **`## [1.0.0] - 2026-03-23`** with notes + known limitations.
- [ ] **Git tag** `v1.0.0` — *you run when publishing* (`git tag v1.0.0 && git push origin v1.0.0`).
- [ ] **GitHub Release** (optional) — copy from `CHANGELOG.md` **1.0.0** section.
- [ ] **`go get github.com/CodeSyncr/nimbus@v1.0.0`** — smoke test *after* tag is pushed and module proxy has the version.

---

## 7. Post-v1 (do not block v1 unless you choose to)

- Wayfinder-style route codegen for TS frontends.
- Full Telescope parity.
- Browser testing (Dusk-equivalent).
- Expanded plugin completion (Scout, Socialite, etc.).

---

## Quick reference — related docs

| Doc | Use |
|-----|-----|
| `GAPS_STATUS.md` | Parity / feature completeness |
| `CHANGELOG.md` | User-facing release notes |
| `README.md` | Install, version pin, stability blurb |
| `.github/workflows/ci.yml` | Required green checks |

---

*Checklist completed for v1.0.0 targeting.*
