# API reference

Use `http://localhost:3000/api` through the gateway or `http://localhost:8080` to call the payment API directly. The gateway removes the `/api` prefix.

All amounts are integer minor currency units (paise for INR). Responses use JSON and UTC RFC 3339 timestamps.

## Health and readiness

| Method | Path | Description |
|---|---|---|
| GET | `/v1/health` | Liveness check |
| GET | `/readyz` | Database readiness check |

## Payments

### Create a payment

`POST /v1/payments`

Requires an `Idempotency-Key` header (1–255 characters).

```json
{
  "amount": 100000,
  "currency": "INR",
  "description": "Order #42",
  "metadata": { "order_id": "ord_abc123" }
}
```

`amount` must be a positive integer, `currency` a 3-letter alphabetic code, and `description` at most 500 characters. Metadata must be a JSON value.

- `201 Created`: new payment stored, initially `CREATED`
- `200 OK`: replay with the same idempotency key and equivalent request
- `409 Conflict`: key reused for a different request
- `422 Unprocessable Entity`: invalid fields

Payment completion happens asynchronously. Possible states are `CREATED`, `PENDING`, `SUCCEEDED`, `FAILED`, and `UNKNOWN`.

### Retrieve a payment

`GET /v1/payments/{paymentID}`

Returns the payment or `404 Not Found`.

### List payments

`GET /v1/payments?limit=20&cursor={paymentID}&status=SUCCEEDED`

`limit` defaults to 20 and is restricted to 1–100. `cursor` is the ID from the last returned payment. `status` is optional.

```json
{
  "data": [],
  "next_cursor": "",
  "total": 0
}
```

### Create a refund

`POST /v1/payments/{paymentID}/refunds`

```json
{ "amount": 50000, "reason": "Customer request" }
```

Refunds are allowed only for succeeded payments. Pending and succeeded refunds count against the refundable balance. Refund completion is asynchronous.

### List refunds

`GET /v1/payments/{paymentID}/refunds`

Returns `{ "data": [...] }`.

## Reconciliation

`GET /v1/reconciliation` returns ledger-balance and unresolved-payment counts:

```json
{
  "unbalanced_ledger_events": 0,
  "unresolved_payments": 0,
  "balanced": true
}
```

## Error format

```json
{ "error": "ValidationError", "message": "..." }
```

Common status codes: `400` malformed JSON, `404` missing resource, `409` idempotency conflict, `422` invalid request or over-refund, `429` rate limit, `502` upstream unavailable, and `500` unexpected server error.
