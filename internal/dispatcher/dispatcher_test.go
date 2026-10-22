package dispatcher

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/loducode/wompi-gateway/internal/domain"
	"github.com/loducode/wompi-gateway/internal/repository/memory"
)

func TestDispatcher_ForwardsExactRawPayloadAndHeaders(t *testing.T) {
	ctx := context.Background()
	repo := memory.New()

	var receivedBody []byte
	var receivedChecksum string
	var receivedCustomHeader string
	var receivedEventID string

	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		receivedChecksum = r.Header.Get("X-Event-Checksum")
		receivedCustomHeader = r.Header.Get("X-Company-Secret")
		receivedEventID = r.Header.Get("X-Wompi-Gateway-Event-ID")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer targetSrv.Close()

	comp := &domain.Company{Name: "Optimax", Slug: "optimax", Active: true}
	_ = repo.CreateCompany(ctx, comp)
	acc := &domain.WompiAccount{Name: "Main Wompi", Slug: "main", Environment: "prod", EventsSecret: "sec", Active: true}
	_ = repo.CreateWompiAccount(ctx, acc)
	rule := &domain.RouteRule{
		WompiAccountID:  acc.ID,
		CompanyID:       comp.ID,
		Name:            "Optimax Sub",
		ReferencePrefix: "sub-",
		TargetURL:       targetSrv.URL,
		CustomHeaders:   map[string]string{"X-Company-Secret": "optimax-token-123"},
		MaxRetries:      3,
		TimeoutSeconds:  5,
		Active:          true,
	}
	_ = repo.CreateRouteRule(ctx, rule)

	rawPayload := []byte(`{"event":"transaction.updated","data":{"transaction":{"id":"tx-1","reference":"sub-001","amount_in_cents":9900000}}}`)
	ev := &domain.WebhookEvent{
		WompiAccountID:    acc.ID,
		EventType:         "transaction.updated",
		TransactionID:     "tx-1",
		Reference:         "sub-001",
		TransactionStatus: "APPROVED",
		Checksum:          "abc123def456",
		RawPayload:        json.RawMessage(rawPayload),
		Status:            domain.StatusPending,
		MatchedCompanyID:  &comp.ID,
		MatchedRouteID:    &rule.ID,
	}
	del := &domain.WebhookDelivery{
		RouteRuleID:    rule.ID,
		CompanyID:      comp.ID,
		TargetURL:      rule.TargetURL,
		CustomHeaders:  rule.CustomHeaders,
		Status:         domain.StatusPending,
		MaxRetries:     3,
		TimeoutSeconds: 5,
		NextRetryAt:    time.Now().UTC().Add(-time.Second),
	}
	savedEv, savedDel, _, err := repo.SaveEventAndDelivery(ctx, ev, del)
	if err != nil {
		t.Fatalf("failed to save event and delivery: %v", err)
	}

	disp := New(repo, Config{
		Concurrency:    2,
		PollInterval:   time.Second,
		DefaultTimeout: 5 * time.Second,
	})

	processed := disp.ProcessDueBatch(ctx)
	if processed != 1 {
		t.Fatalf("expected 1 delivery processed, got %d", processed)
	}

	if string(receivedBody) != string(rawPayload) {
		t.Errorf("expected exact raw payload %s, got %s", string(rawPayload), string(receivedBody))
	}
	if receivedChecksum != "abc123def456" {
		t.Errorf("expected X-Event-Checksum abc123def456, got %s", receivedChecksum)
	}
	if receivedCustomHeader != "optimax-token-123" {
		t.Errorf("expected custom header optimax-token-123, got %s", receivedCustomHeader)
	}
	if receivedEventID != savedEv.ID {
		t.Errorf("expected X-Wompi-Gateway-Event-ID %s, got %s", savedEv.ID, receivedEventID)
	}

	updatedDel, err := repo.GetDeliveryByID(ctx, savedDel.ID)
	if err != nil {
		t.Fatalf("failed to fetch delivery: %v", err)
	}
	if updatedDel.Status != domain.StatusDelivered {
		t.Errorf("expected status delivered, got %s", updatedDel.Status)
	}
	if updatedDel.AttemptCount != 1 {
		t.Errorf("expected attempt count 1, got %d", updatedDel.AttemptCount)
	}
}

func TestDispatcher_RetriesAndTransitionsToDeadLetter(t *testing.T) {
	ctx := context.Background()
	repo := memory.New()

	var callCount atomic.Int32
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer targetSrv.Close()

	comp := &domain.Company{Name: "Mr. Waiters", Slug: "mr-waiters", Active: true}
	_ = repo.CreateCompany(ctx, comp)
	acc := &domain.WompiAccount{Name: "Shared Wompi", Slug: "shared", Environment: "prod", EventsSecret: "sec", Active: true}
	_ = repo.CreateWompiAccount(ctx, acc)
	rule := &domain.RouteRule{
		WompiAccountID:  acc.ID,
		CompanyID:       comp.ID,
		Name:            "Mr Waiters Sub",
		ReferencePrefix: "mw-sub-",
		TargetURL:       targetSrv.URL,
		MaxRetries:      2,
		TimeoutSeconds:  5,
		Active:          true,
	}
	_ = repo.CreateRouteRule(ctx, rule)

	ev := &domain.WebhookEvent{
		WompiAccountID:    acc.ID,
		EventType:         "transaction.updated",
		TransactionID:     "tx-fail-1",
		Reference:         "mw-sub-555",
		TransactionStatus: "APPROVED",
		Checksum:          "deadbeef",
		RawPayload:        json.RawMessage(`{"event":"transaction.updated"}`),
		Status:            domain.StatusPending,
	}
	del := &domain.WebhookDelivery{
		RouteRuleID:    rule.ID,
		CompanyID:      comp.ID,
		TargetURL:      rule.TargetURL,
		Status:         domain.StatusPending,
		MaxRetries:     2,
		TimeoutSeconds: 5,
		NextRetryAt:    time.Now().UTC().Add(-time.Second),
	}
	savedEv, savedDel, _, _ := repo.SaveEventAndDelivery(ctx, ev, del)

	disp := New(repo, Config{
		Concurrency:  1,
		PollInterval: time.Second,
		Backoff: func(attempt int) time.Duration {
			return -1 * time.Second // Immediately eligible for next test batch
		},
	})

	// Attempt 1 -> should transition to retrying
	disp.ProcessDueBatch(ctx)
	d1, _ := repo.GetDeliveryByID(ctx, savedDel.ID)
	if d1.Status != domain.StatusRetrying {
		t.Fatalf("expected status retrying after 1st failed attempt, got %s", d1.Status)
	}

	// Attempt 2 (reaches MaxRetries=2) -> should transition to dead_letter
	disp.ProcessDueBatch(ctx)
	d2, _ := repo.GetDeliveryByID(ctx, savedDel.ID)
	if d2.Status != domain.StatusDeadLetter {
		t.Fatalf("expected status dead_letter after reaching max_retries=2, got %s", d2.Status)
	}

	attempts, _ := repo.ListAttemptsByEventID(ctx, savedEv.ID)
	if len(attempts) != 2 {
		t.Fatalf("expected 2 delivery attempts recorded, got %d", len(attempts))
	}
	if callCount.Load() != 2 {
		t.Fatalf("expected 2 HTTP calls to target server, got %d", callCount.Load())
	}
}
