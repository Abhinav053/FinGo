# FinGo

FinGo is a payment-processing demo built around durable payment state, idempotent requests, asynchronous processing, refunds, and balanced ledger entries.

## Architecture

```
React + Vite (5173)
       |
API Gateway (Express, 3000)
       |
Payment API (Go, 8080) ----- PostgreSQL 16
       |                           |
       +--- transactional outbox --+
                     |
                RabbitMQ 3
                     |
          Payment Worker (Go)
```

| Component | Technology |
|---|---|
| Web dashboard | React, TypeScript, Vite |
| API gateway | Node.js, Express, TypeScript |
| Payment API and worker | Go, Chi |
| Persistence | PostgreSQL |
| Async event delivery | PostgreSQL transactional outbox + RabbitMQ |
| Local stack | Docker Compose |

The provider is simulated. `PROVIDER_DEFAULT_OUTCOME` can be `success`, `failure`, or `timeout`; an unknown timeout outcome is later resolved by a simulated reconciliation result.

## Run locally

Prerequisites: Docker Desktop with Compose, and optionally Go 1.22+ and Node.js 20+ for development outside containers.

From the repository root:

```powershell
Copy-Item .env.example .env
docker compose -f deployments/docker-compose.yml up --build
```

Compose starts PostgreSQL and RabbitMQ, applies migrations, and then starts the API, worker, gateway, and dashboard. For local-only use, the example credentials work; change them before exposing the stack to other users.

| Component | URL |
|---|---|
| Dashboard | http://localhost:5173 |
| Gateway | http://localhost:3000 |
| Payment API | http://localhost:8080 |
| RabbitMQ management | http://localhost:15672 |

The sample credentials for RabbitMQ management are configured by `RABBITMQ_USER` and `RABBITMQ_PASSWORD` in `.env`.

## API quick example

```powershell
curl.exe -X POST http://localhost:3000/api/v1/payments `
  -H "Content-Type: application/json" `
  -H "Idempotency-Key: order-42-attempt-1" `
  -d '{"amount":100000,"currency":"INR","description":"Order #42"}'
```

Amounts are integer minor units (paise for INR). Payment creation is idempotent per key and request body. Read the [API reference](docs/api-reference.md) for listing, refunds, and reconciliation.

## Development

```powershell
# Go API and worker
Set-Location services/payment
go test ./...
go vet ./...
go run ./cmd/api

# API gateway
Set-Location ../api-gateway
npm ci
npm test

# Frontend
Set-Location ../../apps/web
npm ci
npm run dev
```

The Go API and worker require the database schema and a reachable RabbitMQ broker. The Compose stack starts and configures both.

## Project status

Payment creation, idempotency, listing, asynchronous provider simulation, refunds, transactional outbox delivery, ledger balancing, and basic reconciliation are implemented. The demo does not connect to a real payment provider and does not yet include customer-configurable webhook delivery or a production authentication/authorization layer.

## Documentation

- [Architecture](docs/architecture.md)
- [Payment lifecycle](docs/payment-lifecycle.md)
- [API reference](docs/api-reference.md)
- [Database design](docs/database-design.md)
- [Failure handling](docs/failure-handling.md)
- [Implementation status](docs/implementation-log.md)

## License

MIT. See [LICENSE](LICENSE).
