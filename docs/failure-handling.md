# Failure handling

## Idempotent creation

The API requires `Idempotency-Key`, stores a unique key and request hash, and returns the existing payment only when the retry has an equivalent payload. Reusing a key for different payment data returns `409 Conflict`.

## Database and outbox

Payment/refund changes and their outbox messages are committed together. Outbox rows are leased and published to a durable RabbitMQ queue with publisher confirms. A relay failure releases its lease so a later poll can retry. Since the relay can publish before a process crash but before the database row is marked published, delivery is at least once; consumers must be idempotent.

## Worker retries

Queue messages are acknowledged only after successful processing. Errors cause requeue. Payment processing uses a database lease and conditional state updates to recover from worker interruption and ignore completed duplicate messages. Unsupported event types are reported as processing errors and requeued for operator investigation.

## Provider uncertainty

A simulated timeout transitions the payment to `UNKNOWN`, not `FAILED`. A delayed outbox event resolves the result using `PROVIDER_RECONCILE_OUTCOME`. A real provider integration must look up the provider transaction before deciding the terminal state.

## Refunds and ledger

The API locks the payment before checking its refundable balance. This serializes concurrent refund requests and counts both pending and successful refunds against the balance. Refund state and both ledger legs are written transactionally. Each successful payment/refund writes equal debit and credit amounts, protected against duplicate ledger legs by a unique constraint.

## HTTP availability

`/v1/health` reports process liveness, while `/readyz` checks database connectivity. The gateway returns `502` for unavailable payment API responses. Unexpected API/storage failures return a generic `500` response without leaking database details.

## Graceful shutdown

The API stops accepting requests and drains active HTTP work on `SIGINT`/`SIGTERM`. The worker stops its relay and consumer loops and waits up to 30 seconds for them to finish.
