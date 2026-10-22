package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/loducode/wompi-gateway/internal/domain"
)

// Repository is a thread-safe in-memory implementation of domain.Repository.
type Repository struct {
	mu         sync.RWMutex
	companies  map[string]*domain.Company
	accounts   map[string]*domain.WompiAccount
	routes     map[string]*domain.RouteRule
	events     map[string]*domain.WebhookEvent
	deliveries map[string]*domain.WebhookDelivery
	attempts   map[string][]*domain.DeliveryAttempt
}

// New creates a new in-memory Repository.
func New() *Repository {
	return &Repository{
		companies:  make(map[string]*domain.Company),
		accounts:   make(map[string]*domain.WompiAccount),
		routes:     make(map[string]*domain.RouteRule),
		events:     make(map[string]*domain.WebhookEvent),
		deliveries: make(map[string]*domain.WebhookDelivery),
		attempts:   make(map[string][]*domain.DeliveryAttempt),
	}
}

func (r *Repository) CreateCompany(_ context.Context, c *domain.Company) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.companies {
		if strings.EqualFold(existing.Slug, c.Slug) {
			return fmt.Errorf("%w: company slug %q already exists", domain.ErrConflict, c.Slug)
		}
	}

	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.CreatedAt = now
	c.UpdatedAt = now

	cp := *c
	r.companies[c.ID] = &cp
	return nil
}

func (r *Repository) GetCompanyByID(_ context.Context, id string) (*domain.Company, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.companies[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *Repository) ListCompanies(_ context.Context) ([]*domain.Company, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]*domain.Company, 0, len(r.companies))
	for _, c := range r.companies {
		cp := *c
		res = append(res, &cp)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].CreatedAt.Before(res[j].CreatedAt)
	})
	return res, nil
}

func (r *Repository) UpdateCompany(_ context.Context, c *domain.Company) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.companies[c.ID]
	if !ok {
		return domain.ErrNotFound
	}
	for _, other := range r.companies {
		if other.ID != c.ID && strings.EqualFold(other.Slug, c.Slug) {
			return fmt.Errorf("%w: company slug %q already exists", domain.ErrConflict, c.Slug)
		}
	}

	c.CreatedAt = existing.CreatedAt
	c.UpdatedAt = time.Now().UTC()
	cp := *c
	r.companies[c.ID] = &cp
	return nil
}

func (r *Repository) CreateWompiAccount(_ context.Context, a *domain.WompiAccount) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.accounts {
		if strings.EqualFold(existing.Slug, a.Slug) {
			return fmt.Errorf("%w: account slug %q already exists", domain.ErrConflict, a.Slug)
		}
	}

	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.CreatedAt = now
	a.UpdatedAt = now

	cp := *a
	r.accounts[a.ID] = &cp
	return nil
}

func (r *Repository) GetWompiAccountByID(_ context.Context, id string) (*domain.WompiAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.accounts[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *Repository) GetWompiAccountBySlug(_ context.Context, slug string) (*domain.WompiAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, a := range r.accounts {
		if strings.EqualFold(a.Slug, slug) {
			cp := *a
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *Repository) ListWompiAccounts(_ context.Context) ([]*domain.WompiAccount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]*domain.WompiAccount, 0, len(r.accounts))
	for _, a := range r.accounts {
		cp := *a
		res = append(res, &cp)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].CreatedAt.Before(res[j].CreatedAt)
	})
	return res, nil
}

func (r *Repository) UpdateWompiAccount(_ context.Context, a *domain.WompiAccount) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.accounts[a.ID]
	if !ok {
		return domain.ErrNotFound
	}
	for _, other := range r.accounts {
		if other.ID != a.ID && strings.EqualFold(other.Slug, a.Slug) {
			return fmt.Errorf("%w: account slug %q already exists", domain.ErrConflict, a.Slug)
		}
	}

	a.CreatedAt = existing.CreatedAt
	a.UpdatedAt = time.Now().UTC()
	cp := *a
	r.accounts[a.ID] = &cp
	return nil
}

func (r *Repository) CreateRouteRule(_ context.Context, rule *domain.RouteRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.accounts[rule.WompiAccountID]; !ok {
		return fmt.Errorf("%w: wompi_account_id not found", domain.ErrInvalidInput)
	}
	comp, ok := r.companies[rule.CompanyID]
	if !ok {
		return fmt.Errorf("%w: company_id not found", domain.ErrInvalidInput)
	}

	now := time.Now().UTC()
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if rule.MaxRetries <= 0 {
		rule.MaxRetries = 5
	}
	if rule.TimeoutSeconds <= 0 {
		rule.TimeoutSeconds = 15
	}
	if rule.CustomHeaders == nil {
		rule.CustomHeaders = map[string]string{}
	}
	rule.CompanyName = comp.Name
	rule.CreatedAt = now
	rule.UpdatedAt = now

	cp := cloneRouteRule(rule)
	r.routes[rule.ID] = cp
	return nil
}

func (r *Repository) GetRouteRuleByID(_ context.Context, id string) (*domain.RouteRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rule, ok := r.routes[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := cloneRouteRule(rule)
	if comp, exists := r.companies[cp.CompanyID]; exists {
		cp.CompanyName = comp.Name
	}
	return cp, nil
}

func (r *Repository) ListRouteRules(_ context.Context, accountID string) ([]*domain.RouteRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]*domain.RouteRule, 0, len(r.routes))
	for _, rule := range r.routes {
		if accountID != "" && rule.WompiAccountID != accountID {
			continue
		}
		cp := cloneRouteRule(rule)
		if comp, exists := r.companies[cp.CompanyID]; exists {
			cp.CompanyName = comp.Name
		}
		res = append(res, cp)
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].Priority != res[j].Priority {
			return res[i].Priority > res[j].Priority
		}
		return res[i].CreatedAt.Before(res[j].CreatedAt)
	})
	return res, nil
}

func (r *Repository) ListActiveRouteRulesByAccount(_ context.Context, accountID string) ([]*domain.RouteRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var res []*domain.RouteRule
	for _, rule := range r.routes {
		if rule.WompiAccountID == accountID && rule.Active {
			cp := cloneRouteRule(rule)
			if comp, exists := r.companies[cp.CompanyID]; exists {
				cp.CompanyName = comp.Name
			}
			res = append(res, cp)
		}
	}
	return res, nil
}

func (r *Repository) UpdateRouteRule(_ context.Context, rule *domain.RouteRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.routes[rule.ID]
	if !ok {
		return domain.ErrNotFound
	}
	comp, ok := r.companies[rule.CompanyID]
	if !ok {
		return fmt.Errorf("%w: company_id not found", domain.ErrInvalidInput)
	}

	rule.CreatedAt = existing.CreatedAt
	rule.UpdatedAt = time.Now().UTC()
	rule.CompanyName = comp.Name
	if rule.CustomHeaders == nil {
		rule.CustomHeaders = map[string]string{}
	}
	r.routes[rule.ID] = cloneRouteRule(rule)
	return nil
}

func (r *Repository) DeleteRouteRule(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.routes[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.routes, id)
	return nil
}

func (r *Repository) SaveEventAndDelivery(_ context.Context, event *domain.WebhookEvent, delivery *domain.WebhookDelivery) (*domain.WebhookEvent, *domain.WebhookDelivery, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check idempotency: same account + transaction_id + event_type + transaction_status + checksum
	for _, existing := range r.events {
		if existing.WompiAccountID == event.WompiAccountID &&
			existing.TransactionID == event.TransactionID &&
			existing.EventType == event.EventType &&
			existing.TransactionStatus == event.TransactionStatus &&
			strings.EqualFold(existing.Checksum, event.Checksum) {
			evCopy := cloneEvent(existing)
			var delCopy *domain.WebhookDelivery
			for _, d := range r.deliveries {
				if d.WebhookEventID == existing.ID {
					delCopy = cloneDelivery(d)
					break
				}
			}
			return evCopy, delCopy, false, nil
		}
	}

	now := time.Now().UTC()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	event.ReceivedAt = now
	event.UpdatedAt = now
	if acc, ok := r.accounts[event.WompiAccountID]; ok {
		event.AccountSlug = acc.Slug
	}

	r.events[event.ID] = cloneEvent(event)

	var delCopy *domain.WebhookDelivery
	if delivery != nil {
		if delivery.ID == "" {
			delivery.ID = uuid.NewString()
		}
		delivery.WebhookEventID = event.ID
		delivery.CreatedAt = now
		delivery.UpdatedAt = now
		if delivery.NextRetryAt.IsZero() {
			delivery.NextRetryAt = now
		}
		if comp, ok := r.companies[delivery.CompanyID]; ok {
			delivery.CompanyName = comp.Name
		}
		delivery.WompiAccountID = event.WompiAccountID
		delivery.Reference = event.Reference
		delivery.TransactionID = event.TransactionID
		delivery.EventType = event.EventType
		delivery.RawPayload = append(json.RawMessage(nil), event.RawPayload...)
		delivery.Checksum = event.Checksum

		r.deliveries[delivery.ID] = cloneDelivery(delivery)
		delCopy = cloneDelivery(delivery)
	}

	return cloneEvent(event), delCopy, true, nil
}

func (r *Repository) GetWebhookEventByID(_ context.Context, id string) (*domain.WebhookEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ev, ok := r.events[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := cloneEvent(ev)
	if acc, exists := r.accounts[cp.WompiAccountID]; exists {
		cp.AccountSlug = acc.Slug
	}
	return cp, nil
}

func (r *Repository) ListWebhookEvents(_ context.Context, filter domain.EventFilter) ([]*domain.WebhookEvent, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []*domain.WebhookEvent
	for _, ev := range r.events {
		if filter.AccountID != "" && ev.WompiAccountID != filter.AccountID {
			continue
		}
		if filter.CompanyID != "" && (ev.MatchedCompanyID == nil || *ev.MatchedCompanyID != filter.CompanyID) {
			continue
		}
		if filter.Status != "" && string(ev.Status) != filter.Status {
			continue
		}
		if filter.Reference != "" && !strings.Contains(strings.ToLower(ev.Reference), strings.ToLower(filter.Reference)) {
			continue
		}
		cp := cloneEvent(ev)
		if acc, exists := r.accounts[cp.WompiAccountID]; exists {
			cp.AccountSlug = acc.Slug
		}
		matched = append(matched, cp)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ReceivedAt.After(matched[j].ReceivedAt)
	})

	total := int64(len(matched))
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(matched) {
		return []*domain.WebhookEvent{}, total, nil
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], total, nil
}

func (r *Repository) UpdateWebhookEventRouting(_ context.Context, eventID string, status domain.DeliveryStatus, companyID, routeID *string, delivery *domain.WebhookDelivery) (*domain.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ev, ok := r.events[eventID]
	if !ok {
		return nil, domain.ErrNotFound
	}

	now := time.Now().UTC()
	ev.Status = status
	ev.MatchedCompanyID = companyID
	ev.MatchedRouteID = routeID
	ev.UpdatedAt = now

	if delivery == nil {
		return nil, nil
	}

	if delivery.ID == "" {
		delivery.ID = uuid.NewString()
	}
	delivery.WebhookEventID = ev.ID
	delivery.WompiAccountID = ev.WompiAccountID
	delivery.Reference = ev.Reference
	delivery.TransactionID = ev.TransactionID
	delivery.EventType = ev.EventType
	delivery.RawPayload = append(json.RawMessage(nil), ev.RawPayload...)
	delivery.Checksum = ev.Checksum
	delivery.CreatedAt = now
	delivery.UpdatedAt = now
	if delivery.NextRetryAt.IsZero() {
		delivery.NextRetryAt = now
	}
	if comp, exists := r.companies[delivery.CompanyID]; exists {
		delivery.CompanyName = comp.Name
	}

	r.deliveries[delivery.ID] = cloneDelivery(delivery)
	return cloneDelivery(delivery), nil
}

func (r *Repository) GetDeliveryByID(_ context.Context, id string) (*domain.WebhookDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d, ok := r.deliveries[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r.enrichDeliveryLocked(d), nil
}

func (r *Repository) ListDeliveries(_ context.Context, filter domain.DeliveryFilter) ([]*domain.WebhookDelivery, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []*domain.WebhookDelivery
	for _, d := range r.deliveries {
		enriched := r.enrichDeliveryLocked(d)
		if filter.AccountID != "" && enriched.WompiAccountID != filter.AccountID {
			continue
		}
		if filter.CompanyID != "" && enriched.CompanyID != filter.CompanyID {
			continue
		}
		if filter.Status != "" && string(enriched.Status) != filter.Status {
			continue
		}
		matched = append(matched, enriched)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	total := int64(len(matched))
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(matched) {
		return []*domain.WebhookDelivery{}, total, nil
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], total, nil
}

func (r *Repository) ListDeliveriesByEventID(_ context.Context, eventID string) ([]*domain.WebhookDelivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var res []*domain.WebhookDelivery
	for _, d := range r.deliveries {
		if d.WebhookEventID == eventID {
			res = append(res, r.enrichDeliveryLocked(d))
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].CreatedAt.Before(res[j].CreatedAt)
	})
	return res, nil
}

func (r *Repository) ListAttemptsByEventID(_ context.Context, eventID string) ([]*domain.DeliveryAttempt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := r.attempts[eventID]
	res := make([]*domain.DeliveryAttempt, 0, len(list))
	for _, a := range list {
		cp := *a
		res = append(res, &cp)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].CreatedAt.Before(res[j].CreatedAt)
	})
	return res, nil
}

func (r *Repository) ClaimDueDeliveries(_ context.Context, limit int, claimHorizon time.Time) ([]*domain.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	var due []*domain.WebhookDelivery
	for _, d := range r.deliveries {
		if (d.Status == domain.StatusPending || d.Status == domain.StatusRetrying) && !d.NextRetryAt.After(now) {
			due = append(due, d)
		}
	}

	sort.Slice(due, func(i, j int) bool {
		return due[i].NextRetryAt.Before(due[j].NextRetryAt)
	})

	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}

	claimed := make([]*domain.WebhookDelivery, 0, len(due))
	for _, d := range due {
		d.NextRetryAt = claimHorizon
		d.UpdatedAt = now
		claimed = append(claimed, r.enrichDeliveryLocked(d))
	}

	return claimed, nil
}

func (r *Repository) RecordDeliveryAttempt(_ context.Context, delivery *domain.WebhookDelivery, attempt *domain.DeliveryAttempt) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.deliveries[delivery.ID]
	if !ok {
		return domain.ErrNotFound
	}

	existing.Status = delivery.Status
	existing.AttemptCount = delivery.AttemptCount
	existing.NextRetryAt = delivery.NextRetryAt
	existing.LastStatusCode = delivery.LastStatusCode
	existing.LastError = delivery.LastError
	existing.LastResponseBody = delivery.LastResponseBody
	existing.DeliveredAt = delivery.DeliveredAt
	existing.UpdatedAt = delivery.UpdatedAt

	if ev, exists := r.events[existing.WebhookEventID]; exists {
		ev.Status = delivery.Status
		ev.UpdatedAt = delivery.UpdatedAt
	}

	if attempt != nil {
		cp := *attempt
		r.attempts[existing.WebhookEventID] = append(r.attempts[existing.WebhookEventID], &cp)
	}

	return nil
}

func (r *Repository) ResetDeliveryForRetry(_ context.Context, deliveryID string) (*domain.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.deliveries[deliveryID]
	if !ok {
		return nil, domain.ErrNotFound
	}

	now := time.Now().UTC()
	d.Status = domain.StatusPending
	d.AttemptCount = 0
	d.NextRetryAt = now
	d.UpdatedAt = now
	d.LastError = ""

	if ev, exists := r.events[d.WebhookEventID]; exists {
		ev.Status = domain.StatusPending
		ev.UpdatedAt = now
	}

	return r.enrichDeliveryLocked(d), nil
}

func (r *Repository) GetGatewayStats(_ context.Context) (*domain.GatewayStats, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := &domain.GatewayStats{
		TotalEvents:    int64(len(r.events)),
		TotalCompanies: int64(len(r.companies)),
		TotalAccounts:  int64(len(r.accounts)),
		TotalRoutes:    int64(len(r.routes)),
	}

	for _, ev := range r.events {
		if ev.Status == domain.StatusUnrouted {
			stats.UnroutedEvents++
		}
	}

	for _, d := range r.deliveries {
		switch d.Status {
		case domain.StatusPending:
			stats.PendingDeliveries++
		case domain.StatusDelivered:
			stats.DeliveredCount++
		case domain.StatusRetrying:
			stats.RetryingCount++
		case domain.StatusDeadLetter:
			stats.DeadLetterCount++
		}
	}

	return stats, nil
}

func (r *Repository) enrichDeliveryLocked(d *domain.WebhookDelivery) *domain.WebhookDelivery {
	cp := cloneDelivery(d)
	if comp, exists := r.companies[cp.CompanyID]; exists {
		cp.CompanyName = comp.Name
	}
	if ev, exists := r.events[cp.WebhookEventID]; exists {
		cp.WompiAccountID = ev.WompiAccountID
		cp.Reference = ev.Reference
		cp.TransactionID = ev.TransactionID
		cp.EventType = ev.EventType
		cp.RawPayload = append(json.RawMessage(nil), ev.RawPayload...)
		cp.Checksum = ev.Checksum
	}
	return cp
}

func cloneRouteRule(r *domain.RouteRule) *domain.RouteRule {
	cp := *r
	cp.CustomHeaders = make(map[string]string, len(r.CustomHeaders))
	for k, v := range r.CustomHeaders {
		cp.CustomHeaders[k] = v
	}
	return &cp
}

func cloneEvent(ev *domain.WebhookEvent) *domain.WebhookEvent {
	cp := *ev
	cp.RawPayload = append(json.RawMessage(nil), ev.RawPayload...)
	if ev.Headers != nil {
		cp.Headers = make(map[string]string, len(ev.Headers))
		for k, v := range ev.Headers {
			cp.Headers[k] = v
		}
	}
	if ev.MatchedCompanyID != nil {
		s := *ev.MatchedCompanyID
		cp.MatchedCompanyID = &s
	}
	if ev.MatchedRouteID != nil {
		s := *ev.MatchedRouteID
		cp.MatchedRouteID = &s
	}
	return &cp
}

func cloneDelivery(d *domain.WebhookDelivery) *domain.WebhookDelivery {
	cp := *d
	cp.CustomHeaders = make(map[string]string, len(d.CustomHeaders))
	for k, v := range d.CustomHeaders {
		cp.CustomHeaders[k] = v
	}
	cp.RawPayload = append(json.RawMessage(nil), d.RawPayload...)
	if d.LastStatusCode != nil {
		code := *d.LastStatusCode
		cp.LastStatusCode = &code
	}
	if d.DeliveredAt != nil {
		t := *d.DeliveredAt
		cp.DeliveredAt = &t
	}
	return &cp
}
