# Architecture

```text
Browser: React/Vite :5173
    |
    v
Express gateway :3000  -- /api prefix is removed
    |
    v
Go payment API :8080 ---- PostgreSQL 16
                              |
                      transactional outbox
                              |
                         RabbitMQ 3
                              |
                              v
                       Go payment worker
                              |
                       simulated provider
```

## Responsibilities

- **Web dashboard:** creates payments, displays status and amounts, and submits refunds.
- **API gateway:** CORS, per-IP rate limiting, access logging, and HTTP proxying. It has no payment business logic.
- **Payment API:** validates requests, persists payments/refunds, applies idempotency, and exposes reconciliation status.
- **PostgreSQL:** source of truth for payment state, refunds, outbox events, and double-entry ledger rows.
- **Outbox relay:** publishes durable queue messages with RabbitMQ publisher confirms.
- **Worker:** processes payments/refunds, records terminal states, and writes ledger entries.
- **Provider simulator:** returns the environment-configured outcome; no real payments or card data are accepted.

Compose runs the migration job before the API and worker. API liveness (`/v1/health`) is separate from database readiness (`/readyz`).
