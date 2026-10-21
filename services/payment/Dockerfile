# ─── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependency downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build both binaries.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/payment-api   ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/payment-worker ./cmd/worker

# ─── API runtime stage ────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS api
COPY --from=builder /bin/payment-api /payment-api
EXPOSE 8080
ENTRYPOINT ["/payment-api"]

# ─── Worker runtime stage ─────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12 AS worker
COPY --from=builder /bin/payment-worker /payment-worker
ENTRYPOINT ["/payment-worker"]
