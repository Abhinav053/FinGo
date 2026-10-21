# Migrations

SQL migration files for FinGo, managed by [golang-migrate](https://github.com/golang-migrate/migrate).

## File naming convention

```
<version>_<description>.up.sql    — apply migration
<version>_<description>.down.sql  — revert migration
```

## Migrations are added in:

| Migration | Phase | Description |
|---|---|---|
| `000001` | Core | Payments, refunds, outbox, and ledger tables |

## Running migrations

Docker Compose applies migrations using its one-shot `migrate` service before starting the API and worker.

For manual runs, install the `golang-migrate` CLI, set `DATABASE_URL`, then run `bash scripts/migrate.sh up`.
