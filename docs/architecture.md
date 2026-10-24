# WispPay Architecture and Delivery Record

## Repository baseline (2026-10-24)

The repository contained only a one-line README and `.gitignore`; the latter had a pre-existing local edit and was left unchanged. Go 1.27.1, Node.js 22.23.3, and npm 10.9.9 are available. No `DATABASE_URL`, PayPal, Gemma, session, or public URL configuration variables are present. No MySQL client is installed. PayPal Sandbox and Google Generative Language HTTPS endpoints are reachable from this machine, but credentials are not available. Therefore neither Aiven connectivity nor provider credentials/API behavior can be validated against a real account yet.

## Proposed architecture

- One Go process owns the HTTP listener, API routes, health endpoints, and production web assets. React/TypeScript is built by Vite into an embedded Go asset directory; the frontend dev workflow builds to the same directory and does not start a second web server.
- Transport handlers call narrowly scoped application logic; MySQL persistence uses `database/sql`, `go-sql-driver/mysql`, InnoDB, parameterized SQL, versioned migrations, and short transactions. Database readiness is distinct from liveness.
- Authenticated business ownership comes from a server-side session, never a browser-supplied business identifier. Public invoice access uses a random bearer token whose hash is stored. Invoice totals are computed from catalog prices and snapshots are immutable.
- PayPal integration uses the documented Orders v2 Sandbox API with OAuth client credentials. Only a verified server-side capture or verified, matching webhook can transition an invoice to paid. Payment calls are idempotent and do not hold database transactions across network calls.
- React uses Material UI for components, Tailwind CSS 4 for utilities/layout, AG Grid Community for basic sorting/filtering/pagination, and React Router. Enterprise-only grid features are excluded unless licensing is independently confirmed.
- AI drafting remains a backend-only provider adapter and is gated on confirmation of a supported hosted Gemma inference endpoint/model. Current official Gemma material explains model deployment/inference but does not identify a Gemma model on the Gemini API; no model ID or endpoint will be guessed.

## Dependency decisions

Versions were checked from public package metadata on 2026-10-24: Go MySQL driver `v1.10.1` (Go 1.24+); React `19.3.1`; Vite `8.3.5`; Material UI `9.5.0`; AG Grid React `36.2.0`; Tailwind CSS `4.3.3`; React Router `8.5.0`. The browser code will use native Fetch. The Go driver will require TLS for remote databases and use a small bounded connection pool. Exact versions are pinned by generated lock/module files as implementation proceeds.

## Phases and acceptance criteria

1. **Foundation:** reproducible Go and web builds; single Go listener serves frontend and API; environment configuration; MySQL pool/TLS; health and database readiness; versioned migration runner; graceful shutdown and structured request logging. Acceptance: Go tests/build and npm build pass; health is live; readiness reports unavailable without a configured/reachable database; no secret is required to build.
2. **Identity and workspace:** password hashing, server-side expiring sessions, CSRF, registration/login/logout, tenant-scoped catalog and clients. Acceptance: focused unit and MySQL isolation tests.
3. **Invoices:** server-side decimal totals, persisted line snapshots, high-entropy hashed public links, public invoice page. Acceptance: calculation, authorization, token and integration tests.
4. **PayPal Sandbox:** order creation, approval return, capture verification, signature-verified webhooks, idempotency and concurrency protection. Acceptance: deterministic provider tests plus an actual Sandbox buyer checkout before claiming completion.
5. **Operations and AI:** AG Grid Community over API/database data; AI-assisted draft workflow only after provider/model availability and output schema are verified. Acceptance: frontend tests, actual database-backed grid, and evidence of genuine AI response.
6. **Deployment and release:** Render build/start configuration, Aiven migration run, end-to-end tests, deployment only when accounts/network access are supplied. Acceptance: hosted health checks and recorded Sandbox evidence.

## Official integration evidence and blockers

- PayPal Orders v2 documents `POST /v2/checkout/orders` and `POST /v2/checkout/orders/{id}/capture`, OAuth client credentials, Sandbox base URL `https://api-m.sandbox.paypal.com`, `CAPTURE` intent, payer-action approval links, and `PayPal-Request-Id` idempotency. Capture responses include order status and capture status/amount/currency for server verification. Sources: [Orders v2](https://developer.paypal.com/docs/api/orders/v2/), [create order](https://developer.paypal.com/api/orders/v2/orders-create/), [capture order](https://developer.paypal.com/api/orders/v2/orders-capture/), [OAuth](https://developer.paypal.com/api/rest/authentication/).
- PayPal webhook integration documents the `verify-webhook-signature` postback using the raw received event and configured webhook ID; real Sandbox events require a registered webhook URL and credentials. Sources: [webhook overview](https://developer.paypal.com/api/rest/webhooks/), [webhook integration](https://developer.paypal.com/api/rest/webhooks/rest/).
- `go-sql-driver/mysql` `v1.10.1` documents TLS DSN support and recommends bounded open connections and a connection lifetime under common 5-minute intermediary timeouts. The exact Aiven TLS/CA requirements and host connectivity remain unverified because no Aiven connection details were supplied. Source: [driver documentation](https://pkg.go.dev/github.com/go-sql-driver/mysql).
- AG Grid lists Community as free; filtering, sorting and pagination needed here are available without Enterprise. This project uses Community-only features. Source: [licensing and feature comparison](https://www.ag-grid.com/license-pricing/).
- Google Gemma docs point to downloaded model inference or Vertex AI deployment; current Gemini API examples identify Gemini models, not a hosted Gemma model. A Gemini API key alone is not evidence of Gemma API access, so the AI adapter/model choice is blocked pending a supported provider endpoint/model. Sources: [Gemma getting started](https://ai.google.dev/gemma/docs/get_started), [Gemini API](https://ai.google.dev/api).
- Render's free web services spin down after 15 minutes idle, use ephemeral filesystems, and can be suspended under outbound usage limits. Free services are explicitly described as unsuitable for production. Aiven connectivity from a future Render region/plan cannot be confirmed without service metadata. Source: [Render free services](https://render.com/docs/free).
- The VS Code Marketplace was searched for `APIMatic PayPal Context Plugin`, `PayPal Context MCP`, and `APIMatic context plugin`; it returned no matching official extension. No APIMatic context tool is exposed in this session, so installation/use could not be verified and no APIMatic usage is claimed.

## Sponsor integration record

| Product | Intended use and benefit | Evidence / limitation |
| --- | --- | --- |
| PayPal Sandbox | Real hosted approval and server-verified payment | Official API behavior documented; credentials absent, so no live integration evidence yet. |
| Aiven MySQL | Persistent tenant/invoice/payment storage | Driver selected; connection details absent, so not connected. |
| AG Grid Community | Invoice/payment operations workspace | Free license and required basic features confirmed; operational data integration remains in a later phase. |
| Gemma | Human-reviewed catalog/invoice drafting | No currently documented hosted API model established; not implemented until verified. |
| APIMatic PayPal Context Plugin | Development-time integration guidance | Marketplace search returned no matching official extension; no session tool exposed the plugin; no usage claimed. |
| Render | Single Go web service deployment | Free-tier limitations reviewed; no account/deployment supplied, and free tier is not production-suitable. |
