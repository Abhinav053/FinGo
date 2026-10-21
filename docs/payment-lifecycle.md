# Payment lifecycle

```text
CREATED → PENDING → SUCCEEDED
                  ↘ FAILED
                  ↘ UNKNOWN → SUCCEEDED or FAILED (reconciliation)
```

## Payment state

| State | Meaning |
|---|---|
| `CREATED` | Payment and outbox event committed |
| `PENDING` | Worker is processing the simulated provider request |
| `SUCCEEDED` | Provider simulation confirmed success; ledger recorded |
| `FAILED` | Provider simulation declined the payment |
| `UNKNOWN` | Provider simulation timed out; waiting for reconciliation |

The API writes the payment and an outbox event in one PostgreSQL transaction. An outbox relay publishes a persistent RabbitMQ message and marks the event as published only after publisher confirmation. The worker acknowledges a message only after processing it. Conditional updates and database transactions make duplicate delivery safe.

A provider timeout is not treated as a decline. The payment enters `UNKNOWN`, and a delayed reconciliation event uses `PROVIDER_RECONCILE_OUTCOME` (`success` or `failure`) to resolve this demo workflow. Real integrations must query the provider's authoritative transaction state instead.

## Refund lifecycle

Refunds are separate records with `PENDING`, `SUCCEEDED`, or `FAILED` status and do not overwrite the payment status. Refund creation serializes on the parent payment row and verifies that pending plus completed refund amounts do not exceed the original payment. Refund completion creates its ledger pair transactionally.
