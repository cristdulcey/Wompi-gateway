package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/loducode/wompi-gateway/internal/domain"
)

//go:embed migrations/001_init.sql
var initSchemaSQL string

// Repository implements domain.Repository backed by PostgreSQL via pgxpool.
type Repository struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL Repository.
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Migrate executes the embedded SQL schema migration idempotently.
func (r *Repository) Migrate(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, initSchemaSQL)
	if err != nil {
		return fmt.Errorf("failed to apply database migrations: %w", err)
	}
	return nil
}

func (r *Repository) CreateCompany(ctx context.Context, c *domain.Company) error {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	c.CreatedAt = now
	c.UpdatedAt = now

	_, err := r.pool.Exec(ctx, `
		INSERT INTO companies (id, name, slug, description, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, c.ID, c.Name, c.Slug, c.Description, c.Active, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: company slug %q already exists", domain.ErrConflict, c.Slug)
		}
		return err
	}
	return nil
}

func (r *Repository) GetCompanyByID(ctx context.Context, id string) (*domain.Company, error) {
	var c domain.Company
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, active, created_at, updated_at
		FROM companies WHERE id = $1
	`, id).Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.Active, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repository) ListCompanies(ctx context.Context) ([]*domain.Company, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, slug, description, active, created_at, updated_at
		FROM companies
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.Company, 0)
	for rows.Next() {
		var c domain.Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.Active, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		res = append(res, &c)
	}
	return res, rows.Err()
}

func (r *Repository) UpdateCompany(ctx context.Context, c *domain.Company) error {
	c.UpdatedAt = time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE companies
		SET name = $2, slug = $3, description = $4, active = $5, updated_at = $6
		WHERE id = $1
	`, c.ID, c.Name, c.Slug, c.Description, c.Active, c.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: company slug %q already exists", domain.ErrConflict, c.Slug)
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) CreateWompiAccount(ctx context.Context, a *domain.WompiAccount) error {
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.CreatedAt = now
	a.UpdatedAt = now

	_, err := r.pool.Exec(ctx, `
		INSERT INTO wompi_accounts (id, name, slug, environment, events_secret, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, a.ID, a.Name, a.Slug, a.Environment, a.EventsSecret, a.Active, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: account slug %q already exists", domain.ErrConflict, a.Slug)
		}
		return err
	}
	return nil
}

func (r *Repository) GetWompiAccountByID(ctx context.Context, id string) (*domain.WompiAccount, error) {
	var a domain.WompiAccount
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, environment, events_secret, active, created_at, updated_at
		FROM wompi_accounts WHERE id = $1
	`, id).Scan(&a.ID, &a.Name, &a.Slug, &a.Environment, &a.EventsSecret, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *Repository) GetWompiAccountBySlug(ctx context.Context, slug string) (*domain.WompiAccount, error) {
	var a domain.WompiAccount
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, environment, events_secret, active, created_at, updated_at
		FROM wompi_accounts WHERE LOWER(slug) = LOWER($1)
	`, slug).Scan(&a.ID, &a.Name, &a.Slug, &a.Environment, &a.EventsSecret, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *Repository) ListWompiAccounts(ctx context.Context) ([]*domain.WompiAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, slug, environment, events_secret, active, created_at, updated_at
		FROM wompi_accounts
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.WompiAccount, 0)
	for rows.Next() {
		var a domain.WompiAccount
		if err := rows.Scan(&a.ID, &a.Name, &a.Slug, &a.Environment, &a.EventsSecret, &a.Active, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		res = append(res, &a)
	}
	return res, rows.Err()
}

func (r *Repository) UpdateWompiAccount(ctx context.Context, a *domain.WompiAccount) error {
	a.UpdatedAt = time.Now().UTC()
	tag, err := r.pool.Exec(ctx, `
		UPDATE wompi_accounts
		SET name = $2, slug = $3, environment = $4, events_secret = $5, active = $6, updated_at = $7
		WHERE id = $1
	`, a.ID, a.Name, a.Slug, a.Environment, a.EventsSecret, a.Active, a.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: account slug %q already exists", domain.ErrConflict, a.Slug)
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) CreateRouteRule(ctx context.Context, rule *domain.RouteRule) error {
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
	headersJSON, err := json.Marshal(rule.CustomHeaders)
	if err != nil {
		return err
	}
	rule.CreatedAt = now
	rule.UpdatedAt = now

	_, err = r.pool.Exec(ctx, `
		INSERT INTO route_rules (
			id, wompi_account_id, company_id, name, reference_prefix,
			target_url, custom_headers, max_retries, timeout_seconds,
			priority, active, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`, rule.ID, rule.WompiAccountID, rule.CompanyID, rule.Name, rule.ReferencePrefix,
		rule.TargetURL, headersJSON, rule.MaxRetries, rule.TimeoutSeconds,
		rule.Priority, rule.Active, rule.CreatedAt, rule.UpdatedAt)
	return err
}

func (r *Repository) GetRouteRuleByID(ctx context.Context, id string) (*domain.RouteRule, error) {
	var rule domain.RouteRule
	var headersRaw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT r.id, r.wompi_account_id, r.company_id, COALESCE(c.name, ''),
		       r.name, r.reference_prefix, r.target_url, r.custom_headers,
		       r.max_retries, r.timeout_seconds, r.priority, r.active, r.created_at, r.updated_at
		FROM route_rules r
		LEFT JOIN companies c ON c.id = r.company_id
		WHERE r.id = $1
	`, id).Scan(
		&rule.ID, &rule.WompiAccountID, &rule.CompanyID, &rule.CompanyName,
		&rule.Name, &rule.ReferencePrefix, &rule.TargetURL, &headersRaw,
		&rule.MaxRetries, &rule.TimeoutSeconds, &rule.Priority, &rule.Active, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(headersRaw, &rule.CustomHeaders)
	if rule.CustomHeaders == nil {
		rule.CustomHeaders = map[string]string{}
	}
	return &rule, nil
}

func (r *Repository) ListRouteRules(ctx context.Context, accountID string) ([]*domain.RouteRule, error) {
	query := `
		SELECT r.id, r.wompi_account_id, r.company_id, COALESCE(c.name, ''),
		       r.name, r.reference_prefix, r.target_url, r.custom_headers,
		       r.max_retries, r.timeout_seconds, r.priority, r.active, r.created_at, r.updated_at
		FROM route_rules r
		LEFT JOIN companies c ON c.id = r.company_id
	`
	var args []any
	if accountID != "" {
		query += ` WHERE r.wompi_account_id = $1`
		args = append(args, accountID)
	}
	query += ` ORDER BY r.priority DESC, r.created_at ASC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.RouteRule, 0)
	for rows.Next() {
		var rule domain.RouteRule
		var headersRaw []byte
		if err := rows.Scan(
			&rule.ID, &rule.WompiAccountID, &rule.CompanyID, &rule.CompanyName,
			&rule.Name, &rule.ReferencePrefix, &rule.TargetURL, &headersRaw,
			&rule.MaxRetries, &rule.TimeoutSeconds, &rule.Priority, &rule.Active, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(headersRaw, &rule.CustomHeaders)
		if rule.CustomHeaders == nil {
			rule.CustomHeaders = map[string]string{}
		}
		res = append(res, &rule)
	}
	return res, rows.Err()
}

func (r *Repository) ListActiveRouteRulesByAccount(ctx context.Context, accountID string) ([]*domain.RouteRule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.wompi_account_id, r.company_id, COALESCE(c.name, ''),
		       r.name, r.reference_prefix, r.target_url, r.custom_headers,
		       r.max_retries, r.timeout_seconds, r.priority, r.active, r.created_at, r.updated_at
		FROM route_rules r
		LEFT JOIN companies c ON c.id = r.company_id
		WHERE r.wompi_account_id = $1 AND r.active = TRUE
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.RouteRule, 0)
	for rows.Next() {
		var rule domain.RouteRule
		var headersRaw []byte
		if err := rows.Scan(
			&rule.ID, &rule.WompiAccountID, &rule.CompanyID, &rule.CompanyName,
			&rule.Name, &rule.ReferencePrefix, &rule.TargetURL, &headersRaw,
			&rule.MaxRetries, &rule.TimeoutSeconds, &rule.Priority, &rule.Active, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(headersRaw, &rule.CustomHeaders)
		if rule.CustomHeaders == nil {
			rule.CustomHeaders = map[string]string{}
		}
		res = append(res, &rule)
	}
	return res, rows.Err()
}

func (r *Repository) UpdateRouteRule(ctx context.Context, rule *domain.RouteRule) error {
	rule.UpdatedAt = time.Now().UTC()
	if rule.CustomHeaders == nil {
		rule.CustomHeaders = map[string]string{}
	}
	headersJSON, err := json.Marshal(rule.CustomHeaders)
	if err != nil {
		return err
	}

	tag, err := r.pool.Exec(ctx, `
		UPDATE route_rules
		SET wompi_account_id = $2, company_id = $3, name = $4, reference_prefix = $5,
		    target_url = $6, custom_headers = $7, max_retries = $8, timeout_seconds = $9,
		    priority = $10, active = $11, updated_at = $12
		WHERE id = $1
	`, rule.ID, rule.WompiAccountID, rule.CompanyID, rule.Name, rule.ReferencePrefix,
		rule.TargetURL, headersJSON, rule.MaxRetries, rule.TimeoutSeconds,
		rule.Priority, rule.Active, rule.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) DeleteRouteRule(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM route_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) SaveEventAndDelivery(ctx context.Context, event *domain.WebhookEvent, delivery *domain.WebhookDelivery) (*domain.WebhookEvent, *domain.WebhookDelivery, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	event.ReceivedAt = now
	event.UpdatedAt = now
	if event.Headers == nil {
		event.Headers = map[string]string{}
	}
	headersJSON, _ := json.Marshal(event.Headers)

	var insertedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO webhook_events (
			id, wompi_account_id, event_type, environment, transaction_id,
			reference, transaction_status, amount_in_cents, currency,
			checksum, signature_valid, event_timestamp, raw_payload, headers,
			status, matched_company_id, matched_route_id, received_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (wompi_account_id, transaction_id, event_type, transaction_status, checksum)
		DO NOTHING
		RETURNING id
	`, event.ID, event.WompiAccountID, event.EventType, event.Environment, event.TransactionID,
		event.Reference, event.TransactionStatus, event.AmountInCents, event.Currency,
		event.Checksum, event.SignatureValid, event.EventTimestamp, string(event.RawPayload), headersJSON,
		string(event.Status), event.MatchedCompanyID, event.MatchedRouteID, event.ReceivedAt, event.UpdatedAt,
	).Scan(&insertedID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Duplicate webhook event: fetch existing event and delivery
		_ = tx.Rollback(ctx)
		var existingID string
		err := r.pool.QueryRow(ctx, `
			SELECT id FROM webhook_events
			WHERE wompi_account_id = $1 AND transaction_id = $2 AND event_type = $3 AND transaction_status = $4 AND checksum = $5
		`, event.WompiAccountID, event.TransactionID, event.EventType, event.TransactionStatus, event.Checksum).Scan(&existingID)
		if err != nil {
			return nil, nil, false, err
		}
		existingEv, err := r.GetWebhookEventByID(ctx, existingID)
		if err != nil {
			return nil, nil, false, err
		}
		deliveries, _ := r.ListDeliveriesByEventID(ctx, existingID)
		var firstDel *domain.WebhookDelivery
		if len(deliveries) > 0 {
			firstDel = deliveries[0]
		}
		return existingEv, firstDel, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}

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
		if delivery.CustomHeaders == nil {
			delivery.CustomHeaders = map[string]string{}
		}
		customHeadersJSON, _ := json.Marshal(delivery.CustomHeaders)

		_, err = tx.Exec(ctx, `
			INSERT INTO webhook_deliveries (
				id, webhook_event_id, route_rule_id, company_id, target_url,
				custom_headers, status, attempt_count, max_retries, timeout_seconds,
				next_retry_at, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`, delivery.ID, delivery.WebhookEventID, delivery.RouteRuleID, delivery.CompanyID, delivery.TargetURL,
			customHeadersJSON, string(delivery.Status), delivery.AttemptCount, delivery.MaxRetries,
			delivery.TimeoutSeconds, delivery.NextRetryAt, delivery.CreatedAt, delivery.UpdatedAt)
		if err != nil {
			return nil, nil, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, false, err
	}

	return event, delivery, true, nil
}

func (r *Repository) GetWebhookEventByID(ctx context.Context, id string) (*domain.WebhookEvent, error) {
	var ev domain.WebhookEvent
	var rawStr, statusStr string
	var headersRaw []byte

	err := r.pool.QueryRow(ctx, `
		SELECT e.id, e.wompi_account_id, COALESCE(a.slug, ''), e.event_type, e.environment,
		       e.transaction_id, e.reference, e.transaction_status, e.amount_in_cents, e.currency,
		       e.checksum, e.signature_valid, e.event_timestamp, e.raw_payload, e.headers,
		       e.status, e.matched_company_id, e.matched_route_id, e.received_at, e.updated_at
		FROM webhook_events e
		LEFT JOIN wompi_accounts a ON a.id = e.wompi_account_id
		WHERE e.id = $1
	`, id).Scan(
		&ev.ID, &ev.WompiAccountID, &ev.AccountSlug, &ev.EventType, &ev.Environment,
		&ev.TransactionID, &ev.Reference, &ev.TransactionStatus, &ev.AmountInCents, &ev.Currency,
		&ev.Checksum, &ev.SignatureValid, &ev.EventTimestamp, &rawStr, &headersRaw,
		&statusStr, &ev.MatchedCompanyID, &ev.MatchedRouteID, &ev.ReceivedAt, &ev.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	ev.RawPayload = json.RawMessage(rawStr)
	ev.Status = domain.DeliveryStatus(statusStr)
	_ = json.Unmarshal(headersRaw, &ev.Headers)
	return &ev, nil
}

func (r *Repository) ListWebhookEvents(ctx context.Context, filter domain.EventFilter) ([]*domain.WebhookEvent, int64, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if filter.AccountID != "" {
		conditions = append(conditions, fmt.Sprintf("e.wompi_account_id = $%d", argIdx))
		args = append(args, filter.AccountID)
		argIdx++
	}
	if filter.CompanyID != "" {
		conditions = append(conditions, fmt.Sprintf("e.matched_company_id = $%d", argIdx))
		args = append(args, filter.CompanyID)
		argIdx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("e.status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Reference != "" {
		conditions = append(conditions, fmt.Sprintf("e.reference ILIKE $%d", argIdx))
		args = append(args, "%"+filter.Reference+"%")
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM webhook_events e"+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT e.id, e.wompi_account_id, COALESCE(a.slug, ''), e.event_type, e.environment,
		       e.transaction_id, e.reference, e.transaction_status, e.amount_in_cents, e.currency,
		       e.checksum, e.signature_valid, e.event_timestamp, e.raw_payload, e.headers,
		       e.status, e.matched_company_id, e.matched_route_id, e.received_at, e.updated_at
		FROM webhook_events e
		LEFT JOIN wompi_accounts a ON a.id = e.wompi_account_id
		%s
		ORDER BY e.received_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	res := make([]*domain.WebhookEvent, 0)
	for rows.Next() {
		var ev domain.WebhookEvent
		var rawStr, statusStr string
		var headersRaw []byte
		if err := rows.Scan(
			&ev.ID, &ev.WompiAccountID, &ev.AccountSlug, &ev.EventType, &ev.Environment,
			&ev.TransactionID, &ev.Reference, &ev.TransactionStatus, &ev.AmountInCents, &ev.Currency,
			&ev.Checksum, &ev.SignatureValid, &ev.EventTimestamp, &rawStr, &headersRaw,
			&statusStr, &ev.MatchedCompanyID, &ev.MatchedRouteID, &ev.ReceivedAt, &ev.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		ev.RawPayload = json.RawMessage(rawStr)
		ev.Status = domain.DeliveryStatus(statusStr)
		_ = json.Unmarshal(headersRaw, &ev.Headers)
		res = append(res, &ev)
	}
	return res, total, rows.Err()
}

func (r *Repository) UpdateWebhookEventRouting(ctx context.Context, eventID string, status domain.DeliveryStatus, companyID, routeID *string, delivery *domain.WebhookDelivery) (*domain.WebhookDelivery, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE webhook_events
		SET status = $2, matched_company_id = $3, matched_route_id = $4, updated_at = $5
		WHERE id = $1
	`, eventID, string(status), companyID, routeID, now)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}

	if delivery != nil {
		if delivery.ID == "" {
			delivery.ID = uuid.NewString()
		}
		delivery.WebhookEventID = eventID
		delivery.CreatedAt = now
		delivery.UpdatedAt = now
		if delivery.NextRetryAt.IsZero() {
			delivery.NextRetryAt = now
		}
		if delivery.CustomHeaders == nil {
			delivery.CustomHeaders = map[string]string{}
		}
		headersJSON, _ := json.Marshal(delivery.CustomHeaders)

		_, err = tx.Exec(ctx, `
			INSERT INTO webhook_deliveries (
				id, webhook_event_id, route_rule_id, company_id, target_url,
				custom_headers, status, attempt_count, max_retries, timeout_seconds,
				next_retry_at, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`, delivery.ID, delivery.WebhookEventID, delivery.RouteRuleID, delivery.CompanyID, delivery.TargetURL,
			headersJSON, string(delivery.Status), delivery.AttemptCount, delivery.MaxRetries,
			delivery.TimeoutSeconds, delivery.NextRetryAt, delivery.CreatedAt, delivery.UpdatedAt)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return delivery, nil
}

func (r *Repository) GetDeliveryByID(ctx context.Context, id string) (*domain.WebhookDelivery, error) {
	var d domain.WebhookDelivery
	var statusStr, rawStr string
	var headersRaw []byte

	err := r.pool.QueryRow(ctx, `
		SELECT d.id, d.webhook_event_id, d.route_rule_id, d.company_id, COALESCE(c.name, ''),
		       e.wompi_account_id, e.reference, e.transaction_id, e.event_type,
		       d.target_url, d.custom_headers, d.status, d.attempt_count, d.max_retries,
		       d.timeout_seconds, d.next_retry_at, d.last_status_code, d.last_error,
		       d.last_response_body, d.delivered_at, d.created_at, d.updated_at,
		       e.raw_payload, e.checksum
		FROM webhook_deliveries d
		JOIN webhook_events e ON e.id = d.webhook_event_id
		LEFT JOIN companies c ON c.id = d.company_id
		WHERE d.id = $1
	`, id).Scan(
		&d.ID, &d.WebhookEventID, &d.RouteRuleID, &d.CompanyID, &d.CompanyName,
		&d.WompiAccountID, &d.Reference, &d.TransactionID, &d.EventType,
		&d.TargetURL, &headersRaw, &statusStr, &d.AttemptCount, &d.MaxRetries,
		&d.TimeoutSeconds, &d.NextRetryAt, &d.LastStatusCode, &d.LastError,
		&d.LastResponseBody, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt,
		&rawStr, &d.Checksum,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	d.Status = domain.DeliveryStatus(statusStr)
	d.RawPayload = json.RawMessage(rawStr)
	_ = json.Unmarshal(headersRaw, &d.CustomHeaders)
	return &d, nil
}

func (r *Repository) ListDeliveries(ctx context.Context, filter domain.DeliveryFilter) ([]*domain.WebhookDelivery, int64, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if filter.AccountID != "" {
		conditions = append(conditions, fmt.Sprintf("e.wompi_account_id = $%d", argIdx))
		args = append(args, filter.AccountID)
		argIdx++
	}
	if filter.CompanyID != "" {
		conditions = append(conditions, fmt.Sprintf("d.company_id = $%d", argIdx))
		args = append(args, filter.CompanyID)
		argIdx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("d.status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := `
		SELECT COUNT(*)
		FROM webhook_deliveries d
		JOIN webhook_events e ON e.id = d.webhook_event_id
	` + whereClause
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT d.id, d.webhook_event_id, d.route_rule_id, d.company_id, COALESCE(c.name, ''),
		       e.wompi_account_id, e.reference, e.transaction_id, e.event_type,
		       d.target_url, d.custom_headers, d.status, d.attempt_count, d.max_retries,
		       d.timeout_seconds, d.next_retry_at, d.last_status_code, d.last_error,
		       d.last_response_body, d.delivered_at, d.created_at, d.updated_at
		FROM webhook_deliveries d
		JOIN webhook_events e ON e.id = d.webhook_event_id
		LEFT JOIN companies c ON c.id = d.company_id
		%s
		ORDER BY d.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	res := make([]*domain.WebhookDelivery, 0)
	for rows.Next() {
		var d domain.WebhookDelivery
		var statusStr string
		var headersRaw []byte
		if err := rows.Scan(
			&d.ID, &d.WebhookEventID, &d.RouteRuleID, &d.CompanyID, &d.CompanyName,
			&d.WompiAccountID, &d.Reference, &d.TransactionID, &d.EventType,
			&d.TargetURL, &headersRaw, &statusStr, &d.AttemptCount, &d.MaxRetries,
			&d.TimeoutSeconds, &d.NextRetryAt, &d.LastStatusCode, &d.LastError,
			&d.LastResponseBody, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		d.Status = domain.DeliveryStatus(statusStr)
		_ = json.Unmarshal(headersRaw, &d.CustomHeaders)
		res = append(res, &d)
	}
	return res, total, rows.Err()
}

func (r *Repository) ListDeliveriesByEventID(ctx context.Context, eventID string) ([]*domain.WebhookDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.webhook_event_id, d.route_rule_id, d.company_id, COALESCE(c.name, ''),
		       e.wompi_account_id, e.reference, e.transaction_id, e.event_type,
		       d.target_url, d.custom_headers, d.status, d.attempt_count, d.max_retries,
		       d.timeout_seconds, d.next_retry_at, d.last_status_code, d.last_error,
		       d.last_response_body, d.delivered_at, d.created_at, d.updated_at
		FROM webhook_deliveries d
		JOIN webhook_events e ON e.id = d.webhook_event_id
		LEFT JOIN companies c ON c.id = d.company_id
		WHERE d.webhook_event_id = $1
		ORDER BY d.created_at ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.WebhookDelivery, 0)
	for rows.Next() {
		var d domain.WebhookDelivery
		var statusStr string
		var headersRaw []byte
		if err := rows.Scan(
			&d.ID, &d.WebhookEventID, &d.RouteRuleID, &d.CompanyID, &d.CompanyName,
			&d.WompiAccountID, &d.Reference, &d.TransactionID, &d.EventType,
			&d.TargetURL, &headersRaw, &statusStr, &d.AttemptCount, &d.MaxRetries,
			&d.TimeoutSeconds, &d.NextRetryAt, &d.LastStatusCode, &d.LastError,
			&d.LastResponseBody, &d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		d.Status = domain.DeliveryStatus(statusStr)
		_ = json.Unmarshal(headersRaw, &d.CustomHeaders)
		res = append(res, &d)
	}
	return res, rows.Err()
}

func (r *Repository) ListAttemptsByEventID(ctx context.Context, eventID string) ([]*domain.DeliveryAttempt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, delivery_id, webhook_event_id, attempt_number, target_url,
		       status_code, response_body, error_message, duration_ms, success, created_at
		FROM delivery_attempts
		WHERE webhook_event_id = $1
		ORDER BY created_at ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.DeliveryAttempt, 0)
	for rows.Next() {
		var a domain.DeliveryAttempt
		if err := rows.Scan(
			&a.ID, &a.DeliveryID, &a.WebhookEventID, &a.AttemptNumber, &a.TargetURL,
			&a.StatusCode, &a.ResponseBody, &a.ErrorMessage, &a.DurationMs, &a.Success, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		res = append(res, &a)
	}
	return res, rows.Err()
}

// ClaimDueDeliveries atomically selects and leases due pending/retrying deliveries using FOR UPDATE SKIP LOCKED.
func (r *Repository) ClaimDueDeliveries(ctx context.Context, limit int, claimHorizon time.Time) ([]*domain.WebhookDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		WITH claimed AS (
			SELECT id
			FROM webhook_deliveries
			WHERE status IN ('pending', 'retrying')
			  AND next_retry_at <= NOW()
			ORDER BY next_retry_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE webhook_deliveries d
			SET next_retry_at = $2, updated_at = NOW()
			FROM claimed
			WHERE d.id = claimed.id
			RETURNING d.id, d.webhook_event_id, d.route_rule_id, d.company_id,
			          d.target_url, d.custom_headers, d.status, d.attempt_count,
			          d.max_retries, d.timeout_seconds, d.next_retry_at,
			          d.last_status_code, d.last_error, d.last_response_body,
			          d.delivered_at, d.created_at, d.updated_at
		)
		SELECT u.id, u.webhook_event_id, u.route_rule_id, u.company_id, COALESCE(c.name, ''),
		       e.wompi_account_id, e.reference, e.transaction_id, e.event_type,
		       u.target_url, u.custom_headers, u.status, u.attempt_count,
		       u.max_retries, u.timeout_seconds, u.next_retry_at,
		       u.last_status_code, u.last_error, u.last_response_body,
		       u.delivered_at, u.created_at, u.updated_at,
		       e.raw_payload, e.checksum
		FROM updated u
		JOIN webhook_events e ON e.id = u.webhook_event_id
		LEFT JOIN companies c ON c.id = u.company_id
	`, limit, claimHorizon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make([]*domain.WebhookDelivery, 0)
	for rows.Next() {
		var d domain.WebhookDelivery
		var statusStr, rawStr string
		var headersRaw []byte
		if err := rows.Scan(
			&d.ID, &d.WebhookEventID, &d.RouteRuleID, &d.CompanyID, &d.CompanyName,
			&d.WompiAccountID, &d.Reference, &d.TransactionID, &d.EventType,
			&d.TargetURL, &headersRaw, &statusStr, &d.AttemptCount,
			&d.MaxRetries, &d.TimeoutSeconds, &d.NextRetryAt,
			&d.LastStatusCode, &d.LastError, &d.LastResponseBody,
			&d.DeliveredAt, &d.CreatedAt, &d.UpdatedAt,
			&rawStr, &d.Checksum,
		); err != nil {
			return nil, err
		}
		d.Status = domain.DeliveryStatus(statusStr)
		d.RawPayload = json.RawMessage(rawStr)
		_ = json.Unmarshal(headersRaw, &d.CustomHeaders)
		res = append(res, &d)
	}
	return res, rows.Err()
}

func (r *Repository) RecordDeliveryAttempt(ctx context.Context, delivery *domain.WebhookDelivery, attempt *domain.DeliveryAttempt) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = $2, attempt_count = $3, next_retry_at = $4,
		    last_status_code = $5, last_error = $6, last_response_body = $7,
		    delivered_at = $8, updated_at = $9
		WHERE id = $1
	`, delivery.ID, string(delivery.Status), delivery.AttemptCount, delivery.NextRetryAt,
		delivery.LastStatusCode, delivery.LastError, delivery.LastResponseBody,
		delivery.DeliveredAt, delivery.UpdatedAt)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE webhook_events
		SET status = $2, updated_at = $3
		WHERE id = $1
	`, delivery.WebhookEventID, string(delivery.Status), delivery.UpdatedAt)
	if err != nil {
		return err
	}

	if attempt != nil {
		_, err = tx.Exec(ctx, `
			INSERT INTO delivery_attempts (
				id, delivery_id, webhook_event_id, attempt_number, target_url,
				status_code, response_body, error_message, duration_ms, success, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		`, attempt.ID, attempt.DeliveryID, attempt.WebhookEventID, attempt.AttemptNumber, attempt.TargetURL,
			attempt.StatusCode, attempt.ResponseBody, attempt.ErrorMessage, attempt.DurationMs, attempt.Success, attempt.CreatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) ResetDeliveryForRetry(ctx context.Context, deliveryID string) (*domain.WebhookDelivery, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	var eventID string
	err = tx.QueryRow(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending', attempt_count = 0, next_retry_at = $2, last_error = '', updated_at = $2
		WHERE id = $1
		RETURNING webhook_event_id
	`, deliveryID, now).Scan(&eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE webhook_events
		SET status = 'pending', updated_at = $2
		WHERE id = $1
	`, eventID, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetDeliveryByID(ctx, deliveryID)
}

func (r *Repository) GetGatewayStats(ctx context.Context) (*domain.GatewayStats, error) {
	var stats domain.GatewayStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM webhook_events),
			(SELECT COUNT(*) FROM webhook_events WHERE status = 'unrouted'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'pending'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'delivered'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'retrying'),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE status = 'dead_letter'),
			(SELECT COUNT(*) FROM companies),
			(SELECT COUNT(*) FROM wompi_accounts),
			(SELECT COUNT(*) FROM route_rules)
	`).Scan(
		&stats.TotalEvents,
		&stats.UnroutedEvents,
		&stats.PendingDeliveries,
		&stats.DeliveredCount,
		&stats.RetryingCount,
		&stats.DeadLetterCount,
		&stats.TotalCompanies,
		&stats.TotalAccounts,
		&stats.TotalRoutes,
	)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
