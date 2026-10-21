package http

import (
	"encoding/json"
	"net/http"
	"time"
)

// healthResponse is the JSON shape returned by GET /v1/health.
type healthResponse struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

// healthHandler responds with 200 OK and a JSON health payload.
// It is intentionally lightweight and reports process liveness only; database
// readiness is checked separately by /readyz.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:    "ok",
		Service:   "payment-api",
		Timestamp: time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
