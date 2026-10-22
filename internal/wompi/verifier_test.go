package wompi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestParseAndVerify_ValidSignature(t *testing.T) {
	secret := "prod_events_0a1b2c3d4e5f"
	txID := "1293-1700000000-88211"
	txStatus := "APPROVED"
	amount := int64(2500000000) // Large integer to verify json.UseNumber avoids scientific notation
	timestamp := int64(1700000123)

	rawConcat := fmt.Sprintf("%s%s%d%d%s", txID, txStatus, amount, timestamp, secret)
	sum := sha256.Sum256([]byte(rawConcat))
	expectedChecksum := hex.EncodeToString(sum[:])

	payload := fmt.Sprintf(`{
  "event": "transaction.updated",
  "data": {
    "transaction": {
      "id": %q,
      "amount_in_cents": %d,
      "reference": "sub-optimax-991",
      "customer_email": "customer@example.com",
      "currency": "COP",
      "payment_method_type": "CARD",
      "status": %q
    }
  },
  "environment": "prod",
  "signature": {
    "properties": [
      "transaction.id",
      "transaction.status",
      "transaction.amount_in_cents"
    ],
    "checksum": %q
  },
  "timestamp": %d,
  "sent_at": "2025-02-10T15:00:00.000Z"
}`, txID, amount, txStatus, strings.ToUpper(expectedChecksum), timestamp)

	parsed, err := ParseAndVerify([]byte(payload), expectedChecksum, secret)
	if err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}

	if parsed.TransactionID != txID {
		t.Errorf("expected transaction ID %q, got %q", txID, parsed.TransactionID)
	}
	if parsed.Reference != "sub-optimax-991" {
		t.Errorf("expected reference sub-optimax-991, got %q", parsed.Reference)
	}
	if parsed.AmountInCents != amount {
		t.Errorf("expected amount %d, got %d", amount, parsed.AmountInCents)
	}
	if parsed.Checksum != expectedChecksum {
		t.Errorf("expected normalized checksum %q, got %q", expectedChecksum, parsed.Checksum)
	}
	if string(parsed.RawPayload) != payload {
		t.Errorf("expected exact raw payload bytes to be preserved")
	}
}

func TestParseAndVerify_InvalidSignature(t *testing.T) {
	secret := "prod_events_secret"
	payload := `{
  "event": "transaction.updated",
  "data": {
    "transaction": {
      "id": "tx-1",
      "amount_in_cents": 150000,
      "reference": "mw-sub-100",
      "currency": "COP",
      "status": "DECLINED"
    }
  },
  "environment": "test",
  "signature": {
    "properties": ["transaction.id", "transaction.status", "transaction.amount_in_cents"],
    "checksum": "0000000000000000000000000000000000000000000000000000000000000000"
  },
  "timestamp": 1700000123
}`

	_, err := ParseAndVerify([]byte(payload), "", secret)
	if err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestParseAndVerify_NilPropertyBecomesEmptyString(t *testing.T) {
	secret := "test_events_secret"
	timestamp := int64(1700000500)
	rawConcat := fmt.Sprintf("tx-nilAPPROVED%d%s", timestamp, secret)
	sum := sha256.Sum256([]byte(rawConcat))
	checksum := hex.EncodeToString(sum[:])

	payload := fmt.Sprintf(`{
  "event": "transaction.updated",
  "data": {
    "transaction": {
      "id": "tx-nil",
      "status": "APPROVED",
      "optional_field": null
    }
  },
  "environment": "test",
  "signature": {
    "properties": ["transaction.id", "transaction.optional_field", "transaction.status"],
    "checksum": %q
  },
  "timestamp": %d
}`, checksum, timestamp)

	parsed, err := ParseAndVerify([]byte(payload), checksum, secret)
	if err != nil {
		t.Fatalf("expected nil property to resolve to empty string and verify, got %v", err)
	}
	if parsed.TransactionID != "tx-nil" {
		t.Fatalf("unexpected transaction id: %s", parsed.TransactionID)
	}
}
