#!/usr/bin/env bash
# migrate.sh — Run database migrations using golang-migrate.
#
# Usage:
#   bash scripts/migrate.sh up          # Apply all pending migrations
#   bash scripts/migrate.sh down        # Roll back last migration
#   bash scripts/migrate.sh version     # Print current migration version
#   bash scripts/migrate.sh force N     # Force-set version to N (use with care)
#
# Requires: golang-migrate CLI
#   Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
#
# The DATABASE_URL environment variable must be set, or .env must be present.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
MIGRATIONS_DIR="$ROOT_DIR/migrations"

# Load .env if DATABASE_URL is not already set.
if [[ -z "${DATABASE_URL:-}" ]] && [[ -f "$ROOT_DIR/.env" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "$ROOT_DIR/.env"
  set +a
fi

if [[ -z "${DATABASE_URL:-}" ]]; then
  echo "ERROR: DATABASE_URL is not set. Copy .env.example to .env and configure it." >&2
  exit 1
fi

COMMAND="${1:-up}"

case "$COMMAND" in
  up)
    echo "▶ Applying migrations from $MIGRATIONS_DIR ..."
    migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" up
    echo "✅ Migrations applied."
    ;;
  down)
    echo "▶ Rolling back last migration ..."
    migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" down 1
    echo "✅ Migration rolled back."
    ;;
  version)
    migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" version
    ;;
  force)
    VERSION="${2:?Usage: migrate.sh force <version>}"
    echo "⚠️  Forcing migration version to $VERSION ..."
    migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" force "$VERSION"
    ;;
  *)
    echo "Unknown command: $COMMAND"
    echo "Usage: migrate.sh [up|down|version|force <N>]"
    exit 1
    ;;
esac
