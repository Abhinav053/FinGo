package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Abhinav053/FinGo/services/payment/internal/store"
	"github.com/go-chi/chi/v5"
)

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func createPaymentHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input store.CreatePaymentInput
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "request body must be valid JSON")
			return
		}
		input.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		payment, replay, err := payments.CreatePayment(r.Context(), input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if replay {
			writeJSON(w, http.StatusOK, payment)
			return
		}
		writeJSON(w, http.StatusCreated, payment)
	}
}

func getPaymentHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payment, err := payments.GetPayment(r.Context(), chi.URLParam(r, "paymentID"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, payment)
	}
}

func listPaymentsHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				writeError(w, http.StatusUnprocessableEntity, "ValidationError", "limit must be between 1 and 100")
				return
			}
			limit = parsed
		}
		status := strings.ToUpper(r.URL.Query().Get("status"))
		switch status {
		case "", "CREATED", "PENDING", "SUCCEEDED", "FAILED", "UNKNOWN":
		default:
			writeError(w, http.StatusUnprocessableEntity, "ValidationError", "status is not valid")
			return
		}
		data, next, total, err := payments.ListPayments(r.Context(), limit, r.URL.Query().Get("cursor"), status)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data, "next_cursor": next, "total": total})
	}
}

func createRefundHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Amount int64  `json:"amount"`
			Reason string `json:"reason"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "request body must be valid JSON")
			return
		}
		refund, err := payments.CreateRefund(r.Context(), chi.URLParam(r, "paymentID"), input.Amount, input.Reason)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, refund)
	}
}

func listRefundsHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		refunds, err := payments.ListRefunds(r.Context(), chi.URLParam(r, "paymentID"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": refunds})
	}
}

func readyHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeBound(r)
		defer cancel()
		if err := payments.Ready(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "NotReady", "database is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func reconciliationHandler(payments *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeBound(r)
		defer cancel()
		summary, err := payments.Reconciliation(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", "reconciliation query failed")
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func timeBound(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 2*time.Second)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain a single JSON value")
	}
	return nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "ValidationError", "payment or refund fields are invalid")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "NotFound", "resource not found")
	case errors.Is(err, store.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IdempotencyConflict", "idempotency key already used with a different request payload")
	case errors.Is(err, store.ErrOverRefund):
		writeError(w, http.StatusUnprocessableEntity, "OverRefund", "refund amount exceeds remaining refundable amount")
	case errors.Is(err, store.ErrPaymentNotSucceeded):
		writeError(w, http.StatusUnprocessableEntity, "ValidationError", "only succeeded payments can be refunded")
	default:
		writeError(w, http.StatusInternalServerError, "InternalError", "an unexpected error occurred")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
