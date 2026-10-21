package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeMetadata(t *testing.T) {
	first, err := normalizeMetadata(json.RawMessage(`{"count":9007199254740993,"nested":{"b":2,"a":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeMetadata(json.RawMessage(`{ "nested" : { "a": 1, "b": 2 }, "count": 9007199254740993 }`))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("metadata was not canonicalized:\n%s\n%s", first, second)
	}
}

func TestNormalizeMetadataRequiresObject(t *testing.T) {
	for _, raw := range []string{"null", "[]", `"text"`, "42", `{"invalid":`} {
		if _, err := normalizeMetadata(json.RawMessage(raw)); err == nil {
			t.Errorf("normalizeMetadata(%q) succeeded, expected an error", raw)
		}
	}
}

func TestCreatePaymentRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	payments := New(nil)
	tests := []CreatePaymentInput{
		{Amount: 0, Currency: "INR", IdempotencyKey: "key"},
		{Amount: 1, Currency: "IN", IdempotencyKey: "key"},
		{Amount: 1, Currency: "INR"},
		{Amount: 1, Currency: "INR", IdempotencyKey: "key", Metadata: json.RawMessage("[]")},
	}
	for _, input := range tests {
		if _, _, err := payments.CreatePayment(context.Background(), input); !errors.Is(err, ErrInvalid) {
			t.Errorf("CreatePayment(%+v) error = %v, want ErrInvalid", input, err)
		}
	}
}
