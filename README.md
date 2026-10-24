# WispPay

WispPay is a Go-hosted invoicing and payment-link application. The current increment serves the compiled React app from Go and implements business registration, sign-in, sign-out, and expiring server-side sessions with CSRF protection. Catalog, clients, invoices, payment workflows, and AI drafting are not implemented yet; no simulated payments or sample business records are presented as real data.

## Prerequisites

- Go 1.25 or later
- Node.js 22 or later and npm
- A MySQL-compatible database for readiness and migrations

Copy `.env.example` to `.env` and set the values in your shell or deployment environment. Configure either `DATABASE_URL` (Go MySQL driver DSN or `mysql://` URI) or all of `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_NAME`, `DATABASE_USER`, and `DATABASE_PASSWORD`. TLS certificate verification remains enabled. The app uses `cert/ca.pem` when present, otherwise `DATABASE_CA_CERT` or `SSL_CERT` may specify a CA file path. Keep credentials and certificates out of source control.

## Build and run

```powershell
cd web
npm ci
npm run build
cd ..
go test ./...
New-Item -ItemType Directory -Force tmp
go build -o ./tmp/wisppay.exe ./cmd/wisppay
.\tmp\wisppay.exe
```

The Go server binds to `0.0.0.0:$PORT` (default `18080`) and embeds `web/dist`. During development, `npm run watch` in `web` rebuilds those assets without starting a frontend server. Because Go embeds a snapshot at compile time, rebuild and restart the Go process after a frontend change, then refresh the browser; HMR is not enabled.

`GET /healthz` checks process liveness. `GET /readyz` checks database readiness and returns `503` until MySQL is configured and reachable. Apply versioned migrations explicitly with `go run ./cmd/wisppay -migrate`; normal server startup never changes the database schema. Migrations are not destructive. Run `go test ./...` and `npm run typecheck --prefix web` for the current automated checks; a frontend behavior test suite has not been added yet.

The Render build/start definition is in `render.yaml`. Configure `DATABASE_URL` and secrets in the Render dashboard. Render free web services spin down when idle and have ephemeral storage; the free plan is not suitable for production. Test network connectivity from the chosen Render region to the actual Aiven service before deployment.

## Current external integration status

The additive identity migration is implemented but has not been applied: the configured database endpoint accepts TCP but did not return a valid MySQL handshake during the migration attempt. No schema changes were made by that attempt. PayPal Sandbox OAuth and Gemma inference were verified in an earlier local check, but no order, capture, webhook, or product-drafting workflow is implemented. No Render deployment or end-to-end customer payment is claimed. See [the architecture and phase record](docs/architecture.md) for remaining work and evidence.