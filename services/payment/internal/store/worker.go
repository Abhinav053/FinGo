package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Abhinav053/FinGo/services/payment/internal/queue"
)

func PublishNext(ctx context.Context, db *sql.DB, publish func(context.Context, queue.Event) error) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	var event queue.Event
	err = tx.QueryRowContext(ctx, `
		SELECT id, event_type, payload FROM outbox_events
		WHERE published = FALSE AND available_at <= NOW() AND (claimed_until IS NULL OR claimed_until < NOW())
		ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&event.ID, &event.EventType, &event.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return false, nil
	}
	if err != nil {
		tx.Rollback()
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE outbox_events SET claimed_until = NOW() + INTERVAL '30 seconds' WHERE id = $1", event.ID); err != nil {
		tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}

	if err := publish(ctx, event); err != nil {
		_, releaseErr := db.ExecContext(context.WithoutCancel(ctx), "UPDATE outbox_events SET claimed_until = NULL WHERE id = $1 AND published = FALSE", event.ID)
		if releaseErr != nil {
			return false, fmt.Errorf("publish event: %w; release outbox claim: %v", err, releaseErr)
		}
		return false, err
	}
	if _, err := db.ExecContext(ctx, "UPDATE outbox_events SET published = TRUE, claimed_until = NULL WHERE id = $1", event.ID); err != nil {
		return false, fmt.Errorf("mark event published: %w", err)
	}
	return true, nil
}

func ProcessEvent(ctx context.Context, db *sql.DB, event queue.Event, outcome, reconcileOutcome string, delay time.Duration) error {
	switch event.EventType {
	case "payment.created":
		return processPayment(ctx, db, event.Payload, outcome, delay)
	case "payment.reconcile":
		return reconcilePayment(ctx, db, event.Payload, reconcileOutcome)
	case "refund.created":
		return processRefund(ctx, db, event.Payload)
	default:
		return fmt.Errorf("unsupported outbox event type %q", event.EventType)
	}
}

func processPayment(ctx context.Context, db *sql.DB, payloadBytes []byte, outcome string, delay time.Duration) (returnErr error) {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var status string
	var lease sql.NullTime
	err = tx.QueryRowContext(ctx, "SELECT status, processing_until FROM payments WHERE id = $1 FOR UPDATE", payload.ID).Scan(&status, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return ErrNotFound
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	if status == "SUCCEEDED" || status == "FAILED" || status == "UNKNOWN" {
		return tx.Commit()
	}
	if status == "PENDING" && lease.Valid && lease.Time.After(time.Now()) {
		return tx.Commit()
	}
	leaseDuration := delay + time.Minute
	if leaseDuration < time.Minute {
		leaseDuration = time.Minute
	}
	if _, err := tx.ExecContext(ctx, "UPDATE payments SET status = 'PENDING', processing_until = NOW() + ($2 * INTERVAL '1 millisecond'), updated_at = NOW() WHERE id = $1", payload.ID, leaseDuration.Milliseconds()); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	completed := false
	defer func() {
		if !completed {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, cleanupErr := db.ExecContext(cleanupCtx, "UPDATE payments SET processing_until = NOW() WHERE id = $1 AND status = 'PENDING'", payload.ID)
			if cleanupErr != nil && returnErr == nil {
				returnErr = fmt.Errorf("release payment processing lease: %w", cleanupErr)
			}
		}
	}()

	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
	}

	finalStatus := "SUCCEEDED"
	var failureReason any
	if outcome == "failure" {
		finalStatus = "FAILED"
		failureReason = "payment declined by simulated provider"
	} else if outcome == "timeout" {
		finalStatus = "UNKNOWN"
		failureReason = "provider response timed out; outcome requires reconciliation"
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE payments SET status = $2, failure_reason = $3, provider_ref = $4,
		processing_until = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'PENDING'`, payload.ID, finalStatus, failureReason, fmt.Sprintf("sim_%s", payload.ID))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 && finalStatus == "SUCCEEDED" {
		amount, err := mustGetAmount(ctx, tx, payload.ID)
		if err != nil {
			return err
		}
		if err := insertLedgerPair(ctx, tx, payload.ID, "customer_funds", "provider_clearing", amount); err != nil {
			return err
		}
	}
	if changed == 1 && finalStatus == "UNKNOWN" {
		reconcilePayload, err := json.Marshal(map[string]string{"id": payload.ID})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO outbox_events (aggregate_id, event_type, payload, available_at)
			VALUES ($1, 'payment.reconcile', $2, NOW() + INTERVAL '5 seconds')`, payload.ID, reconcilePayload); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	completed = true
	return nil
}

func reconcilePayment(ctx context.Context, db *sql.DB, payloadBytes []byte, outcome string) error {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	finalStatus := "SUCCEEDED"
	var failureReason any
	if outcome == "failure" {
		finalStatus = "FAILED"
		failureReason = "simulated provider reconciliation confirmed failure"
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE payments SET status = $2, failure_reason = $3, processing_until = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'UNKNOWN'`, payload.ID, finalStatus, failureReason)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 && finalStatus == "SUCCEEDED" {
		amount, err := mustGetAmount(ctx, tx, payload.ID)
		if err != nil {
			return err
		}
		if err := insertLedgerPair(ctx, tx, payload.ID, "customer_funds", "provider_clearing", amount); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func processRefund(ctx context.Context, db *sql.DB, payloadBytes []byte) error {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var paymentID string
	var amount int64
	err = tx.QueryRowContext(ctx, `UPDATE refunds SET status = 'SUCCEEDED', processing_until = NULL, updated_at = NOW() WHERE id = $1 AND status = 'PENDING' RETURNING payment_id, amount`, payload.ID).Scan(&paymentID, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if err := insertLedgerPair(ctx, tx, payload.ID, "refunds_payable", "customer_funds", amount); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE payments SET updated_at = NOW() WHERE id = $1", paymentID); err != nil {
		return err
	}
	return tx.Commit()
}

func insertLedgerPair(ctx context.Context, tx *sql.Tx, eventID, debitAccount, creditAccount string, amount int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (event_id, account, direction, amount)
		VALUES ($1, $2, 'DEBIT', $4), ($1, $3, 'CREDIT', $4)
		ON CONFLICT (event_id, account, direction) DO NOTHING`, eventID, debitAccount, creditAccount, amount)
	return err
}

func mustGetAmount(ctx context.Context, tx *sql.Tx, paymentID string) (int64, error) {
	var amount int64
	err := tx.QueryRowContext(ctx, "SELECT amount FROM payments WHERE id = $1", paymentID).Scan(&amount)
	return amount, err
}
