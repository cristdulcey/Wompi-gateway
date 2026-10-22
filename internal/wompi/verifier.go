package wompi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrInvalidJSON indicates the webhook body is not valid JSON.
	ErrInvalidJSON = errors.New("invalid wompi webhook JSON payload")
	// ErrMissingSignature indicates signature metadata or checksum is missing.
	ErrMissingSignature = errors.New("missing wompi signature properties or checksum")
	// ErrInvalidSignature indicates the computed SHA256 checksum does not match the event checksum.
	ErrInvalidSignature = errors.New("invalid wompi event signature checksum")
)

// ParsedEvent holds the extracted Wompi webhook fields along with the raw payload.
type ParsedEvent struct {
	EventType         string
	Environment       string
	Timestamp         int64
	Checksum          string
	SignatureProps    []string
	TransactionID     string
	Reference         string
	TransactionStatus string
	AmountInCents     int64
	Currency          string
	Data              map[string]any
	RawPayload        []byte
}

type rawEnvelope struct {
	Event       string         `json:"event"`
	Environment string         `json:"environment"`
	Timestamp   json.Number    `json:"timestamp"`
	Signature   rawSignature   `json:"signature"`
	Data        map[string]any `json:"data"`
}

type rawSignature struct {
	Properties []string `json:"properties"`
	Checksum   string   `json:"checksum"`
}

// ParseAndVerify parses a raw Wompi webhook JSON payload using json.UseNumber()
// to avoid floating-point distortion on large integers (such as amount_in_cents),
// extracts transaction metadata, and verifies the SHA256 checksum against eventsSecret.
func ParseAndVerify(rawPayload []byte, headerChecksum string, eventsSecret string) (*ParsedEvent, error) {
	parsed, err := Parse(rawPayload)
	if err != nil {
		return nil, err
	}

	expectedChecksum := strings.TrimSpace(headerChecksum)
	if expectedChecksum == "" {
		expectedChecksum = strings.TrimSpace(parsed.Checksum)
	}
	if expectedChecksum == "" || len(parsed.SignatureProps) == 0 {
		return nil, ErrMissingSignature
	}

	// If both header and body checksums are present, ensure they do not contradict each other.
	if headerChecksum != "" && parsed.Checksum != "" && !equalHex(headerChecksum, parsed.Checksum) {
		return nil, ErrInvalidSignature
	}

	computed := ComputeChecksumFromParsed(parsed, eventsSecret)
	if !equalHex(computed, expectedChecksum) {
		return nil, ErrInvalidSignature
	}

	parsed.Checksum = strings.ToLower(expectedChecksum)
	return parsed, nil
}

// Parse decodes the raw Wompi webhook JSON payload using json.Decoder with UseNumber().
func Parse(rawPayload []byte) (*ParsedEvent, error) {
	if len(bytes.TrimSpace(rawPayload)) == 0 {
		return nil, ErrInvalidJSON
	}

	dec := json.NewDecoder(bytes.NewReader(rawPayload))
	dec.UseNumber()

	var env rawEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	var ts int64
	if env.Timestamp != "" {
		parsedTS, err := env.Timestamp.Int64()
		if err != nil {
			return nil, fmt.Errorf("%w: invalid timestamp %q", ErrInvalidJSON, env.Timestamp.String())
		}
		ts = parsedTS
	}

	txID := lookupString(env.Data, "transaction.id")
	ref := lookupString(env.Data, "transaction.reference")
	txStatus := lookupString(env.Data, "transaction.status")
	currency := lookupString(env.Data, "transaction.currency")

	var amountInCents int64
	if rawAmount := lookupString(env.Data, "transaction.amount_in_cents"); rawAmount != "" {
		if v, err := strconv.ParseInt(rawAmount, 10, 64); err == nil {
			amountInCents = v
		}
	}

	rawCopy := make([]byte, len(rawPayload))
	copy(rawCopy, rawPayload)

	return &ParsedEvent{
		EventType:         env.Event,
		Environment:       env.Environment,
		Timestamp:         ts,
		Checksum:          strings.TrimSpace(env.Signature.Checksum),
		SignatureProps:    env.Signature.Properties,
		TransactionID:     txID,
		Reference:         ref,
		TransactionStatus: txStatus,
		AmountInCents:     amountInCents,
		Currency:          currency,
		Data:              env.Data,
		RawPayload:        rawCopy,
	}, nil
}

// ComputeChecksum parses a raw payload and computes its lowercased SHA256 hex checksum.
func ComputeChecksum(rawPayload []byte, eventsSecret string) (string, error) {
	parsed, err := Parse(rawPayload)
	if err != nil {
		return "", err
	}
	return ComputeChecksumFromParsed(parsed, eventsSecret), nil
}

// ComputeChecksumFromParsed concatenates each property in signature.properties looked up under data,
// followed by the event timestamp and eventsSecret, and returns the SHA256 hex digest.
func ComputeChecksumFromParsed(parsed *ParsedEvent, eventsSecret string) string {
	var b strings.Builder
	for _, prop := range parsed.SignatureProps {
		b.WriteString(lookupString(parsed.Data, prop))
	}
	b.WriteString(strconv.FormatInt(parsed.Timestamp, 10))
	b.WriteString(eventsSecret)

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func lookupString(data map[string]any, dotPath string) string {
	if data == nil || dotPath == "" {
		return ""
	}
	parts := strings.Split(dotPath, ".")
	var current any = data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		val, exists := m[part]
		if !exists || val == nil {
			return ""
		}
		current = val
	}

	switch v := current.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

func equalHex(a, b string) bool {
	cleanA := strings.ToLower(strings.TrimSpace(a))
	cleanB := strings.ToLower(strings.TrimSpace(b))
	return hmac.Equal([]byte(cleanA), []byte(cleanB))
}
