// Package http wires up the Chi router and all HTTP handlers.
package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/Abhinav053/FinGo/services/payment/internal/store"
)

// NewRouter constructs and returns the root Chi router with all middleware
// and route groups registered.
func NewRouter(log zerolog.Logger, payments *store.Store) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ────────────────────────────────────────────────────
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(zerologMiddleware(log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// ── Health ───────────────────────────────────────────────────────────────
	r.Get("/v1/health", healthHandler)
	r.Get("/readyz", readyHandler(payments))
	r.Get("/v1/reconciliation", reconciliationHandler(payments))

	r.Post("/v1/payments", createPaymentHandler(payments))
	r.Get("/v1/payments", listPaymentsHandler(payments))
	r.Get("/v1/payments/{paymentID}", getPaymentHandler(payments))
	r.Post("/v1/payments/{paymentID}/refunds", createRefundHandler(payments))
	r.Get("/v1/payments/{paymentID}/refunds", listRefundsHandler(payments))

	return r
}

// zerologMiddleware returns a Chi-compatible middleware that logs each request
// using the provided zerolog.Logger.
func zerologMiddleware(log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				log.Info().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("request_id", middleware.GetReqID(r.Context())).
					Int("status", ww.Status()).
					Dur("latency", time.Since(start)).
					Msg("request")
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
