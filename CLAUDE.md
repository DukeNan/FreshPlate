# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Read `AGENTS.md` for project structure, style conventions, and PR guidelines. This file focuses on the big-picture architecture and invariants that require reading multiple files to understand. Repository docs (including all design/contract/test documents) are written in Simplified Chinese; commit subjects follow the existing English imperative style (`feat:`, `docs:`, `test(e2e):`).

## Development Commands

```bash
# Backend (Go 1.26.4 via goenv; run from server/)
go build ./... && go vet ./... && go test ./...        # full backend checks
go test -race -count=1 ./internal/...                  # race detector
go test ./internal/api/ -run TestFoodAPI021            # single test
go test ./internal/api/ -run 'TestFoodAPI' -v          # one family, verbose

# Frontend (run from web/)
npm ci                          # install locked deps (once)
npm run dev                     # Vite at 127.0.0.1:5173 (proxies /api → :8080)
npm test                        # Vitest unit tests
npm run build                   # production build to dist/

# E2E (run from web/; boots an isolated server on 127.0.0.1:18080 with a throwaway DB)
npx playwright install chromium          # once; on macOS ≤12 this fails → use system Chrome:
FRESHPLATE_E2E_GO=<go 1.26.4 path> \
FRESHPLATE_E2E_CHROMIUM='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' \
  npm run test:e2e

# CLI/process-lock verification (repo root; needs system python3 ≥ 3.7)
python3 scripts/verify_cli.py --go go

# Swagger: annotations live on handlers; regenerate after changing them
(cd server/internal/api && go generate .)   # writes server/docs/{docs.go,swagger.json,swagger.yaml}
```

Environment note: on machines where the shell exports a stale `GOROOT`, every `go` command fails with `compile: version "..." does not match` — bypass with `env -u GOROOT <path-to-go1.26.4>/go`. A stale `GOROOT` also poisons the e2e harness, hence `FRESHPLATE_E2E_GO`.

## Architecture

Three-layer backend, no ORM: `internal/api` (Gin handlers, DTOs, middleware) → `internal/logic` (pure validation, no IO) → `internal/store` (all SQL). Gin handlers never touch SQL directly; `logic` never opens connections.

### Write-path invariant (the core pattern, `internal/store/*`)

Every business write runs in `WithImmediate` (single pooled connection, `BEGIN IMMEDIATE`) and must follow this order:

1. `checkSession` re-verifies session, account status, forced-password-change, and role **inside the transaction** (a demotion/disable mid-request fails here, not in middleware).
2. Read the current row and compare `version` (409 `VERSION_CONFLICT` on mismatch).
3. Re-check cross-resource state in the same transaction (e.g. referenced food must be active).
4. Conditional update: `UPDATE ... WHERE id=? AND version=?` — `RowsAffected()` must be exactly 1.
5. Audit rows (only for public resources) are written in the same transaction; any error rolls back everything.

Ownership scoping is part of the WHERE clause (`user_id=? AND deleted_at IS NULL` for inventory, `visibility='private' AND owner_user_id=?` for private recipes). Cross-tenant access must return 404 (indistinguishable from missing); insufficient role is 403 from middleware.

### Migration chain (append-only)

`store/db.go` holds a declarative `migrationChain` (001→005). Never modify an already-applied migration file — startup verifies per-version SHA-256 checksums, contiguous history, and rejects future/unknown schemas. New modules append `NNN_name.sql` to `server/migrations/` and extend the chain plus the startup structure checks (tables/columns/indexes) and `migrate_test.go`. Both `cmd/server` and `cmd/admin` must open the new version.

### Request body limits (single source)

`bodyLimit(fullPath, method)` in `internal/api` decides 64 KiB (default) vs 256 KiB (the four recipe write routes, matched by gin route template + POST/PUT). The `context` middleware pre-check, `MaxBytesReader`, and decode must all derive from it — never widen only the decoder.

### Contracts

- Success `{"data":...}`, pagination `{"items":[],"total":N}` (default page 20, max 100, `id DESC`), errors `{"error":{"code","message"}}` via `store.E(status, code, message)`.
- Names are matched with parameterized literal substring `instr(name, ?)>0` — never SQL LIKE wildcards.
- Quantities are JSON decimal **strings** (≤3 fraction digits) stored as milli-integers; parsing never goes through float64. Backend: `logic/quantity.go`; frontend mirror: `web/src/quantity.js` (kept in sync by `web/src/quantity.test.js`).
- `decodeFoodInput` enforces "all keys must be present; explicit `null` means unknown": missing key → 400, key with null → stored as NULL. `TestFoodAPI024` keeps `swagger.json` `required` in sync with the backend key set — if you change the required-key set, update the DTO tags and regenerate Swagger.
- Two audit tables with distinct purposes: `audit_logs` (account operations, `target_user_id NOT NULL`) and `resource_audit_logs` (food / public-recipe changes only; private recipes and inventory are never audited).

### Auth & identity

Access JWT (15 min) resolved against the session on every request; refresh cookie rotates with replay detection. `protected(admin)` middleware handles 401/403 and validates `X-Expected-User-ID` on writes (409 `IDENTITY_CHANGED`). The frontend tracks an `auth.generation` counter (id/role/force-change changes) — async view code snapshots the generation and drops stale responses; never auto-retry writes on network error or 409.

### Frontend conventions

No new npm dependencies. Form views copy `PreferencesView.vue`'s skeleton (expectedUserId/generation snapshot, `verifyForm`, `role="alert"`, disabled fieldset while busy); list views copy `UsersView.vue` (request-sequence counter, filters form, pagination). Views that switch edit targets must reset form state immediately and snapshot the submit target/`version` **before** any `await` (see `FoodsView.vue` `save()` — this class of race was fixed in review; don't regress it).

### Tests & traceability

Test case baselines live in `docs/test/*_TEST_CASES.md`; every Go/Vitest/E2E test function carries the case ID it implements (e.g. `// FOOD-API-021`), and `docs/test/TEST_EXECUTION_PLAN.md` maps case ranges → test files → commands. When adding behavior, extend the relevant case doc and follow the same ID convention. The implementation summary (`docs/test/IMPLEMENTATION_SUMMARY.md`) records verified results.

### Compatibility red lines

- `GET /api/foods` response shape (id/name/status array of active foods, ascending, unpaged) is frozen — the preferences page depends on it.
- The 12 seeded `foods` rows keep their IDs forever; semantic renames are forbidden (create + disable instead).
- Sessions, `user_preferences`, and existing audit records must survive every migration (`migrate_test.go` asserts this).
