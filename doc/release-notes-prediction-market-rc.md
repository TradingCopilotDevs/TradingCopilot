# Prediction Market RC Release Notes

Date: 2026-06-12

## Release Candidate Scope

- Adds Polymarket v1 public prediction-market support, including discovery, market snapshots, order book and price-history evidence tools, news-to-market matching, watchlist items, provider health, setup readiness, and a frontend prediction-market page.
- Adds prediction-market research-team isolation with `assetClass=prediction_market`, paper-account-free default prediction teams, default prediction filters, and observation-only meeting recaps.
- Keeps v1 limited to public market data and research workflows. It does not add wallets, API keys, real orders, deposits, withdrawals, positions, or prediction-market paper trading.

## Verification

- Passed: `go generate ./...`
- Passed: `npm run typegen:api --prefix frontend`
- Passed: `npm ci --prefix frontend`
- Passed: `go test ./...`
- Passed: `go vet ./...`
- Passed: `golangci-lint run`
- Passed: `npm run build --prefix frontend`
- Passed: `git diff --check` with Windows LF/CRLF warnings only

## Known Release Gates

- `npm audit --prefix frontend --audit-level=moderate` reports 2 moderate vulnerabilities through `vite -> esbuild`. The advisory affects the Vite development server. The available automated fix requires `npm audit fix --force` and upgrades Vite to `8.0.16`, which is a breaking major-version jump. This RC keeps the current Vite version and records the issue as a dev-server-only known risk.
- `docker compose config` could not be executed in the local release-check environment because Docker CLI is not installed. This remains a required gate for CI or the release machine before publishing.
- In-app browser automation could not inspect `http://127.0.0.1:5173/login` because the browser policy blocked the local URL. Manual UI verification is still required for login, setup wizard, research teams, prediction markets, and provider health pages.

## Release Recommendation

This build is suitable to cut as a release candidate after committing the scoped changes. Do not publish a final release until Docker Compose validation and manual UI verification have been completed in an environment that supports them.
