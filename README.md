# WispPay

WispPay is a Go-hosted invoicing and payment-link demo. It serves the compiled React app from Go and stores account, catalog, client, invoice, and public-link data in a local SQLite database. The app can use PayPal Sandbox for test seller onboarding and checkout; sandbox actions are external test transactions and do not move real money. The demo does not connect to a remote database.

## Prerequisites

- Node.js 22.22 or later and npm
- No database service is required. SQLite is embedded in the Go server.

Copy `.env.example` to `.env` if desired. `SQLITE_PATH` selects the SQLite file and defaults to `data/wisppay.db`. The application creates the parent directory and initializes its schema on startup. Legacy `DATABASE_*` values are ignored.

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

`GET /healthz` checks process liveness. `GET /readyz` checks the local SQLite database. SQLite schema initialization is automatic and local; the server does not run remote MySQL migrations. Run `go test ./...` and `npm run typecheck --prefix web` for the automated checks.

The Render build/start definition is in `render.yaml`. It uses `data/wisppay.db` on the service filesystem. Render services without an attached persistent disk have ephemeral storage, so demo accounts and invoices may be lost on restart or redeploy. Set PayPal Sandbox client credentials, partner IDs, and the public origin in the Render dashboard to enable onboarding and checkout. The webhook ID is optional for checkout; without it, webhook verification is unavailable. This setup is for demos, not durable production data.

## Current external integration status

The prior MySQL implementation and migrations remain in the repository but are not used by this local demo. No Render deployment or real customer payment is claimed. See [the architecture and phase record](docs/architecture.md) for remaining work and evidence.