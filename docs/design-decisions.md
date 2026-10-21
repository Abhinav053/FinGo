# Design decisions

## Integer amounts

Payment amounts are positive integer minor units. This avoids floating-point rounding in persistence and the ledger. The dashboard performs currency formatting only for display.

## Opaque payment IDs

IDs use a resource prefix and 128 random bits (for example `pay_<hex>`). They are URL-safe and do not expose sequential transaction counts. Sorting uses the database creation timestamp with the ID as a deterministic tie-breaker.

## PostgreSQL transactions

PostgreSQL is the source of truth. `database/sql` with pgx's standard-library driver keeps queries explicit and allows transactions, row locking, and conditional updates without ORM behavior.

## Transactional outbox

The API writes payment/refund state and its outbox event atomically. A worker relay publishes to RabbitMQ using persistent messages and publisher confirms. Consumer work is idempotent because message delivery is at least once.

## Separate API and worker binaries

Both binaries share the same Go packages while scaling independently: the API handles bounded HTTP requests; workers handle provider simulation and ledger updates asynchronously.

## Simulated payments

The provider is intentionally simulated to avoid handling card details, test credentials, or real funds. Environment configuration can exercise success, decline, and timeout/reconciliation paths.

## Thin gateway

Node/Express handles cross-origin policy, request logging, rate limiting, and proxying only. Payment rules remain in the Go service.
