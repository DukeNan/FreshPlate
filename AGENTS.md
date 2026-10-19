# Repository Guidelines

## Project Structure & Module Organization

FreshPlate currently implements user accounts, dietary preferences, the shared food catalog, personal inventory, and recipe management (see `docs/test/IMPLEMENTATION_SUMMARY.md`); recommendation, meal logging, and nutrition features remain planned.

- `server/cmd/server/` and `server/cmd/admin/`: HTTP service and maintenance CLI entry points.
- `server/internal/`: `api` handles HTTP/security, `logic` owns business rules, `store` manages SQLite, and `runtime` handles configuration and process locks.
- `server/migrations/`: embedded SQL migrations.
- `web/src/`: Vue components, views, shared JavaScript, styles, and colocated unit tests. `web/e2e/` contains browser tests and fixtures; `web/dist/` is generated output.
- `scripts/`: CLI/process verification. `docs/`: architecture, contracts, test plans, and verification records.

## Build, Test, and Development Commands

Use Go 1.26.4, Node.js 20.19+, and npm; browser/CLI checks also require Python 3. Commands start at the repository root:

- `npm ci --prefix web`: install locked frontend dependencies.
- `npm run dev --prefix web`: start Vite at `127.0.0.1:5173`.
- `npm run build --prefix web`: build production assets.
- `(cd server && go run ./cmd/server --origin http://127.0.0.1:5173)`: start the development API.
- `(cd server && go build ./cmd/...)`: compile backend entry points.
- `(cd server && go test ./... && go vet ./... && go test -race ./...)`: backend checks.
- `npm test --prefix web`: run Vitest.
- `(cd web && npx playwright install chromium && npm run test:e2e)`: browser checks using an isolated service on port 18080.
- `python3 scripts/verify_cli.py --go go`: verify CLI recovery and process locking.

## Coding Style & Naming Conventions

Format Go with `gofmt` (tabs); use exported PascalCase and unexported camelCase names. Follow existing JavaScript: two-space indentation, single quotes, and generally omitted semicolons. Name Vue views `PascalCaseView.vue`. No ESLint or Prettier configuration is present. Reuse shared API/auth helpers and preserve API → logic → store responsibilities.

## Testing Guidelines

Use Go `testing` in `*_test.go`, Vitest in `*.test.js`, and Playwright in `web/e2e/*.spec.js`. Add regression coverage for changed behavior, especially authentication, authorization, transactions, and identity changes. No numeric coverage threshold is configured. Keep tests isolated from everyday databases.

## Commit & Pull Request Guidelines

History uses short imperative subjects with prefixes such as `feat:` and `docs:`. Follow that pattern. PRs should describe the problem, resulting behavior, relevant issues, and checks run; include screenshots for UI changes and flag schema/configuration impacts.

## Security & Configuration Tips

Use `.env.example` as reference; `.env` is not automatically loaded. Never commit databases, CSRF keys, or credentials. Run maintenance with the service stopped. Match the browser origin exactly; `localhost` and `127.0.0.1` differ.
