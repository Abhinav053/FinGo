package http

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Abhinav053/FinGo/services/payment/internal/store"
)

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid object", body: `{"amount":42}`},
		{name: "malformed json", body: `{"amount":`, wantErr: true},
		{name: "unknown field", body: `{"amount":42,"other":true}`, wantErr: true},
		{name: "trailing value", body: `{"amount":42} {}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			var value struct {
				Amount int `json:"amount"`
			}
			err := decodeJSON(rec, req, &value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWriteStoreError(t *testing.T) {
	tests := []struct {
		err        error
		statusCode int
		code       string
	}{
		{store.ErrInvalid, http.StatusUnprocessableEntity, "ValidationError"},
		{store.ErrNotFound, http.StatusNotFound, "NotFound"},
		{store.ErrIdempotencyConflict, http.StatusConflict, "IdempotencyConflict"},
		{store.ErrOverRefund, http.StatusUnprocessableEntity, "OverRefund"},
		{errors.New("database unavailable"), http.StatusInternalServerError, "InternalError"},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		writeStoreError(rec, tt.err)
		if rec.Code != tt.statusCode {
			t.Fatalf("status = %d, want %d", rec.Code, tt.statusCode)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"`+tt.code+`"`)) {
			t.Fatalf("response body %q does not contain error code %q", rec.Body.String(), tt.code)
		}
	}
}
