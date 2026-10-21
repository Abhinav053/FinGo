package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrIdempotencyConflict = errors.New("idempotency key already used with a different request")
	ErrInvalid             = errors.New("invalid request")
	ErrOverRefund          = errors.New("refund amount exceeds remaining refundable amount")
	ErrPaymentNotSucceeded = errors.New("payment is not succeeded")
)

type Store struct {
	db *sql.DB
}

type Payment struct {
	ID             string          `json:"id"`
	Amount         int64           `json:"amount"`
	Currency       string          `json:"currency"`
	Status         string          `json:"status"`
	Description    string          `json:"description"`
	IdempotencyKey string          `json:"idempotency_key"`
	Metadata       json.RawMessage `json:"metadata"`
	ProviderRef    *string         `json:"provider_ref,omitempty"`
	FailureReason  *string         `json:"failure_reason"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreatePaymentInput struct {
	Amount         int64           `json:"amount"`
	Currency       string          `json:"currency"`
	Description    string          `json:"description"`
	IdempotencyKey string          `json:"idempotency_key"`
	Metadata       json.RawMessage `json:"metadata"`
}

type Refund struct {
	ID        string    `json:"id"`
	PaymentID string    `json:"payment_id"`
	Amount    int64     `json:"amount"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type ReconciliationSummary struct {
	UnbalancedLedgerEvents int  `json:"unbalanced_ledger_events"`
	UnresolvedPayments     int  `json:"unresolved_payments"`
	Balanced               bool `json:"balanced"`
}

func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Ready(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) Reconciliation(ctx context.Context) (ReconciliationSummary, error) {
	var summary ReconciliationSummary
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM (
			SELECT event_id FROM ledger_entries
			GROUP BY event_id
			HAVING SUM(CASE WHEN direction = 'DEBIT' THEN amount ELSE -amount END) <> 0
		) unbalanced`).Scan(&summary.UnbalancedLedgerEvents); err != nil {
		return ReconciliationSummary{}, err
	}
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM payments WHERE status = 'UNKNOWN'").Scan(&summary.UnresolvedPayments); err != nil {
		return ReconciliationSummary{}, err
	}
	summary.Balanced = summary.UnbalancedLedgerEvents == 0
	return summary, nil
}

func (s *Store) CreatePayment(ctx context.Context, input CreatePaymentInput) (Payment, bool, error) {
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Amount < 1 || len(input.Currency) != 3 || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 255 {
		return Payment{}, false, ErrInvalid
	}
	for _, char := range input.Currency {
		if char < 'A' || char > 'Z' {
			return Payment{}, false, ErrInvalid
		}
	}
	if len(input.Description) > 500 {
		return Payment{}, false, ErrInvalid
	}
	if len(input.Metadata) == 0 {
		input.Metadata = json.RawMessage("{}")
	}
	metadataBytes, err := normalizeMetadata(input.Metadata)
	if err != nil {
		return Payment{}, false, ErrInvalid
	}
	input.Metadata = metadataBytes
	requestBytes, err := json.Marshal(struct {
		Amount      int64           `json:"amount"`
		Currency    string          `json:"currency"`
		Description string          `json:"description"`
		Metadata    json.RawMessage `json:"metadata"`
	}{input.Amount, input.Currency, input.Description, input.Metadata})
	if err != nil {
		return Payment{}, false, fmt.Errorf("encode payment request: %w", err)
	}
	requestHash := fmt.Sprintf("%x", sha256.Sum256(requestBytes))
	id, err := newID("pay")
	if err != nil {
		return Payment{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Payment{}, false, err
	}
	defer tx.Rollback()

	payment, err := scanPayment(tx.QueryRowContext(ctx, `
		INSERT INTO payments (id, idempotency_key, request_hash, amount, currency, description, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, amount, currency, status, description, idempotency_key, metadata, provider_ref, failure_reason, created_at, updated_at`,
		id, input.IdempotencyKey, requestHash, input.Amount, input.Currency, input.Description, input.Metadata))
	created := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		payment, err = scanPayment(tx.QueryRowContext(ctx, `
			SELECT id, amount, currency, status, description, idempotency_key, metadata, provider_ref, failure_reason, created_at, updated_at
			FROM payments WHERE idempotency_key = $1`, input.IdempotencyKey))
		if err != nil {
			return Payment{}, false, fmt.Errorf("read idempotent payment: %w", err)
		}
		var storedHash string
		if err := tx.QueryRowContext(ctx, "SELECT request_hash FROM payments WHERE id = $1", payment.ID).Scan(&storedHash); err != nil {
			return Payment{}, false, err
		}
		if storedHash != requestHash {
			return Payment{}, false, ErrIdempotencyConflict
		}
	} else if err != nil {
		return Payment{}, false, fmt.Errorf("insert payment: %w", err)
	}
	if created {
		payload, _ := json.Marshal(map[string]string{"id": payment.ID})
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events (aggregate_id, event_type, payload) VALUES ($1, 'payment.created', $2)`, payment.ID, payload); err != nil {
			return Payment{}, false, fmt.Errorf("enqueue payment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Payment{}, false, fmt.Errorf("commit payment: %w", err)
	}
	return payment, !created, nil
}

func normalizeMetadata(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var metadata any
	if err := decoder.Decode(&metadata); err != nil {
		return nil, err
	}
	if _, ok := metadata.(map[string]any); !ok {
		return nil, ErrInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(canonical), nil
}

func (s *Store) GetPayment(ctx context.Context, id string) (Payment, error) {
	payment, err := scanPayment(s.db.QueryRowContext(ctx, `
		SELECT id, amount, currency, status, description, idempotency_key, metadata, provider_ref, failure_reason, created_at, updated_at
		FROM payments WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Payment{}, ErrNotFound
	}
	return payment, err
}

func (s *Store) ListPayments(ctx context.Context, limit int, cursor, status string) ([]Payment, string, int, error) {
	if limit < 1 || limit > 100 {
		return nil, "", 0, ErrInvalid
	}
	var cursorTime sql.NullTime
	if cursor != "" {
		err := s.db.QueryRowContext(ctx, "SELECT created_at FROM payments WHERE id = $1", cursor).Scan(&cursorTime)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", 0, ErrNotFound
		}
		if err != nil {
			return nil, "", 0, err
		}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM payments WHERE ($1 = '' OR status = $1)`, status).Scan(&total); err != nil {
		return nil, "", 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, amount, currency, status, description, idempotency_key, metadata, provider_ref, failure_reason, created_at, updated_at
		FROM payments
		WHERE ($1 = '' OR status = $1)
		  AND ($2 = '' OR (created_at, id) < ($3, $2))
		ORDER BY created_at DESC, id DESC LIMIT $4`, status, cursor, cursorTime.Time, limit+1)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()
	payments := make([]Payment, 0, limit)
	for rows.Next() {
		payment, err := scanPayment(rows)
		if err != nil {
			return nil, "", 0, err
		}
		payments = append(payments, payment)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, err
	}
	next := ""
	if len(payments) > limit {
		payments = payments[:limit]
		next = payments[len(payments)-1].ID
	}
	return payments, next, total, nil
}

func (s *Store) CreateRefund(ctx context.Context, paymentID string, amount int64, reason string) (Refund, error) {
	if amount < 1 || len(reason) > 500 {
		return Refund{}, ErrInvalid
	}
	id, err := newID("ref")
	if err != nil {
		return Refund{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Refund{}, err
	}
	defer tx.Rollback()
	var status string
	var paymentAmount int64
	if err := tx.QueryRowContext(ctx, `SELECT status, amount FROM payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&status, &paymentAmount); errors.Is(err, sql.ErrNoRows) {
		return Refund{}, ErrNotFound
	} else if err != nil {
		return Refund{}, err
	}
	if status != "SUCCEEDED" {
		return Refund{}, ErrPaymentNotSucceeded
	}
	var refunded int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(amount), 0) FROM refunds WHERE payment_id = $1 AND status != 'FAILED'`, paymentID).Scan(&refunded); err != nil {
		return Refund{}, err
	}
	if amount > paymentAmount-refunded {
		return Refund{}, ErrOverRefund
	}
	refund := Refund{ID: id, PaymentID: paymentID, Amount: amount, Status: "PENDING", Reason: reason}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO refunds (id, payment_id, amount, reason) VALUES ($1, $2, $3, $4)
		RETURNING created_at`, refund.ID, refund.PaymentID, refund.Amount, refund.Reason).Scan(&refund.CreatedAt); err != nil {
		return Refund{}, err
	}
	payload, _ := json.Marshal(map[string]string{"id": refund.ID})
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events (aggregate_id, event_type, payload) VALUES ($1, 'refund.created', $2)`, paymentID, payload); err != nil {
		return Refund{}, err
	}
	if err := tx.Commit(); err != nil {
		return Refund{}, err
	}
	return refund, nil
}

func (s *Store) ListRefunds(ctx context.Context, paymentID string) ([]Refund, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM payments WHERE id = $1)", paymentID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, payment_id, amount, status, reason, created_at FROM refunds WHERE payment_id = $1 ORDER BY created_at`, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refunds := make([]Refund, 0)
	for rows.Next() {
		var refund Refund
		if err := rows.Scan(&refund.ID, &refund.PaymentID, &refund.Amount, &refund.Status, &refund.Reason, &refund.CreatedAt); err != nil {
			return nil, err
		}
		refunds = append(refunds, refund)
	}
	return refunds, rows.Err()
}

type rowScanner interface {
	Scan(...any) error
}

func scanPayment(row rowScanner) (Payment, error) {
	var payment Payment
	var metadata []byte
	var providerRef, failureReason sql.NullString
	err := row.Scan(&payment.ID, &payment.Amount, &payment.Currency, &payment.Status, &payment.Description,
		&payment.IdempotencyKey, &metadata, &providerRef, &failureReason, &payment.CreatedAt, &payment.UpdatedAt)
	if err != nil {
		return Payment{}, err
	}
	payment.Metadata = json.RawMessage(metadata)
	if providerRef.Valid {
		payment.ProviderRef = &providerRef.String
	}
	if failureReason.Valid {
		payment.FailureReason = &failureReason.String
	}
	return payment, nil
}

func newID(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(random), nil
}
