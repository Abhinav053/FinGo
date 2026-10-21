# Database design

Migrations live in `migrations/` and are applied automatically by the Compose `migrate` service. IDs are opaque text values; amounts use integer minor currency units.

## Core tables

### `payments`

One row for each payment attempt. A unique `idempotency_key` and SHA-256 `request_hash` protect repeated API requests. A positive amount, 3-letter currency, known status, JSONB metadata, provider outcome, and timestamps are stored with the payment. `processing_until` is a worker lease so a timed-out worker job can be retried.

### `refunds`

Refund rows reference their payment and have `PENDING`, `SUCCEEDED`, or `FAILED` status. The API locks the parent payment row before summing pending and completed refunds, preventing concurrent over-refunds.

### `outbox_events`

Payment and refund records are written in the same transaction as their outbox event. The relay publishes durable RabbitMQ messages with publisher confirms, then marks those events published. `claimed_until` coordinates relay replicas; `available_at` schedules delayed reconciliation events.

### `ledger_entries`

Each completed payment/refund event produces equal debit and credit entries in one transaction. A unique constraint on `(event_id, account, direction)` prevents duplicate ledger legs. Reconciliation groups the entries by event and reports any non-zero debit/credit difference.

## Money and consistency

- Persist money as `BIGINT` in minor units; never use floating-point arithmetic for stored amounts.
- Enforce positive amounts and supported statuses with database checks.
- Keep payment creation, outbox insertion, terminal state changes, and ledger updates transactional.
- Treat provider timeouts as `UNKNOWN`, not as a failure; reconciliation resolves the demo provider outcome later.
