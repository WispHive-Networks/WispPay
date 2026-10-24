# WispPay

WispPay is a Go-hosted invoicing and payment-link application. The repository is at the foundation stage: the Go server serves the compiled React application, provides liveness/readiness endpoints, and supports versioned MySQL migrations. Authentication, business workflows, invoices, payments, and AI drafting are not implemented yet; no simulated payments or sample business records are presented as real data.

## Prerequisites

- Go 1.25 or later
- Node.js 22 or later and npm
- A MySQL-compatible database for readiness and migrations

Copy `.env.example` to `.env` and set the values in your shell or deployment environment. `DATABASE_URL` accepts either a Go MySQL driver DSN or a `mysql://` URI with `ssl-mode=REQUIRED`, `VERIFY_CA`, or `VERIFY_IDENTITY`. The app uses `cert/ca.pem` when present unless `DATABASE_CA_CERT` overrides it; TLS certificate verification remains enabled. Keep credentials out of source control.

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

PayPal Sandbox, Aiven MySQL, and Gemma are not connected in this foundation build. No provider credentials or Aiven settings were present during implementation, so no Sandbox purchase, database migration against Aiven, Render deployment, or Gemma inference is claimed. Gemma API availability/model naming needs a verified hosted inference target; do not populate `GEMMA_MODEL` with an assumed identifier. See [the architecture and phase record](docs/architecture.md) for official sources, decisions, remaining acceptance criteria, and sponsor status.