package domain

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a requested resource does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrConflict is returned when a uniqueness constraint is violated.
	ErrConflict = errors.New("resource already exists")
	// ErrInvalidInput is returned when an entity fails domain validation.
	ErrInvalidInput = errors.New("invalid input")
)

// DeliveryStatus represents the lifecycle state of a webhook event or delivery.
type DeliveryStatus string

const (
	StatusPending    DeliveryStatus = "pending"
	StatusDelivered  DeliveryStatus = "delivered"
	StatusRetrying   DeliveryStatus = "retrying"
	StatusDeadLetter DeliveryStatus = "dead_letter"
	StatusUnrouted   DeliveryStatus = "unrouted"
)

// Company represents a downstream tenant or organization (e.g., Optimax, Mr. Waiters).
type Company struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WompiAccount represents a configured Wompi merchant account whose webhooks are received at /webhooks/wompi/:slug.
type WompiAccount struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Environment  string    `json:"environment"` // "prod" or "test"
	EventsSecret string    `json:"events_secret,omitempty"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RouteRule defines how an incoming Wompi event is routed to a Company's TargetURL based on transaction reference prefix.
type RouteRule struct {
	ID              string            `json:"id"`
	WompiAccountID  string            `json:"wompi_account_id"`
	CompanyID       string            `json:"company_id"`
	CompanyName     string            `json:"company_name,omitempty"`
	Name            string            `json:"name"`
	ReferencePrefix string            `json:"reference_prefix"` // e.g. "sub-", "mw-sub-", or "" / "*" for fallback
	TargetURL       string            `json:"target_url"`
	CustomHeaders   map[string]string `json:"custom_headers"`
	MaxRetries      int               `json:"max_retries"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	Priority        int               `json:"priority"`
	Active          bool              `json:"active"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// WebhookEvent stores the immutable raw webhook received from Wompi along with extracted metadata.
type WebhookEvent struct {
	ID                string          `json:"id"`
	WompiAccountID    string          `json:"wompi_account_id"`
	AccountSlug       string          `json:"account_slug,omitempty"`
	EventType         string          `json:"event_type"`
	Environment       string          `json:"environment"`
	TransactionID     string          `json:"transaction_id"`
	Reference         string          `json:"reference"`
	TransactionStatus string          `json:"transaction_status"`
	AmountInCents     int64           `json:"amount_in_cents"`
	Currency          string          `json:"currency"`
	Checksum          string          `json:"checksum"`
	SignatureValid    bool            `json:"signature_valid"`
	EventTimestamp    int64           `json:"event_timestamp"`
	RawPayload        json.RawMessage `json:"raw_payload"`
	Headers           map[string]string `json:"headers"`
	Status            DeliveryStatus  `json:"status"`
	MatchedCompanyID  *string         `json:"matched_company_id,omitempty"`
	MatchedRouteID    *string         `json:"matched_route_id,omitempty"`
	ReceivedAt        time.Time       `json:"received_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// WebhookDelivery tracks the delivery state of a WebhookEvent to a specific RouteRule target.
type WebhookDelivery struct {
	ID               string            `json:"id"`
	WebhookEventID   string            `json:"webhook_event_id"`
	RouteRuleID      string            `json:"route_rule_id"`
	CompanyID        string            `json:"company_id"`
	CompanyName      string            `json:"company_name,omitempty"`
	WompiAccountID   string            `json:"wompi_account_id,omitempty"`
	Reference        string            `json:"reference,omitempty"`
	TransactionID    string            `json:"transaction_id,omitempty"`
	EventType        string            `json:"event_type,omitempty"`
	TargetURL        string            `json:"target_url"`
	CustomHeaders    map[string]string `json:"custom_headers"`
	Status           DeliveryStatus    `json:"status"`
	AttemptCount     int               `json:"attempt_count"`
	MaxRetries       int               `json:"max_retries"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	NextRetryAt      time.Time         `json:"next_retry_at"`
	LastStatusCode   *int              `json:"last_status_code,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	LastResponseBody string            `json:"last_response_body,omitempty"`
	DeliveredAt      *time.Time        `json:"delivered_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`

	// Embedded event fields populated when claiming a due delivery for forwarding.
	RawPayload json.RawMessage `json:"raw_payload,omitempty"`
	Checksum   string          `json:"checksum,omitempty"`
}

// DeliveryAttempt records an individual HTTP POST attempt made by the dispatcher worker.
type DeliveryAttempt struct {
	ID             string    `json:"id"`
	DeliveryID     string    `json:"delivery_id"`
	WebhookEventID string    `json:"webhook_event_id"`
	AttemptNumber  int       `json:"attempt_number"`
	TargetURL      string    `json:"target_url"`
	StatusCode     *int      `json:"status_code,omitempty"`
	ResponseBody   string    `json:"response_body"`
	ErrorMessage   string    `json:"error_message"`
	DurationMs     int64     `json:"duration_ms"`
	Success        bool      `json:"success"`
	CreatedAt      time.Time `json:"created_at"`
}

// GatewayStats aggregates webhook event and delivery metrics for the dashboard and monitoring.
type GatewayStats struct {
	TotalEvents       int64 `json:"total_events"`
	UnroutedEvents    int64 `json:"unrouted_events"`
	PendingDeliveries int64 `json:"pending_deliveries"`
	DeliveredCount    int64 `json:"delivered_count"`
	RetryingCount     int64 `json:"retrying_count"`
	DeadLetterCount   int64 `json:"dead_letter_count"`
	TotalCompanies    int64 `json:"total_companies"`
	TotalAccounts     int64 `json:"total_accounts"`
	TotalRoutes       int64 `json:"total_routes"`
}

// EventFilter specifies query filters for listing webhook events.
type EventFilter struct {
	AccountID string
	CompanyID string
	Reference string
	Status    string
	Limit     int
	Offset    int
}

// DeliveryFilter specifies query filters for listing webhook deliveries.
type DeliveryFilter struct {
	AccountID string
	CompanyID string
	Status    string
	Limit     int
	Offset    int
}

// EventDetail bundles a WebhookEvent with its deliveries and delivery attempts.
type EventDetail struct {
	Event      *WebhookEvent      `json:"event"`
	Deliveries []*WebhookDelivery `json:"deliveries"`
	Attempts   []*DeliveryAttempt `json:"attempts"`
}
