package domain

import (
	"context"
	"time"
)

// Repository defines the persistence port for the Wompi Webhook Gateway.
type Repository interface {
	// Companies
	CreateCompany(ctx context.Context, c *Company) error
	GetCompanyByID(ctx context.Context, id string) (*Company, error)
	ListCompanies(ctx context.Context) ([]*Company, error)
	UpdateCompany(ctx context.Context, c *Company) error

	// Wompi Accounts
	CreateWompiAccount(ctx context.Context, a *WompiAccount) error
	GetWompiAccountByID(ctx context.Context, id string) (*WompiAccount, error)
	GetWompiAccountBySlug(ctx context.Context, slug string) (*WompiAccount, error)
	ListWompiAccounts(ctx context.Context) ([]*WompiAccount, error)
	UpdateWompiAccount(ctx context.Context, a *WompiAccount) error

	// Route Rules
	CreateRouteRule(ctx context.Context, r *RouteRule) error
	GetRouteRuleByID(ctx context.Context, id string) (*RouteRule, error)
	ListRouteRules(ctx context.Context, accountID string) ([]*RouteRule, error)
	ListActiveRouteRulesByAccount(ctx context.Context, accountID string) ([]*RouteRule, error)
	UpdateRouteRule(ctx context.Context, r *RouteRule) error
	DeleteRouteRule(ctx context.Context, id string) error

	// Webhook Events & Deliveries
	// SaveEventAndDelivery persists an incoming webhook event (idempotently) and its initial delivery if routed.
	// Returns the persisted event, optional delivery, a boolean indicating whether the event was newly created (false if duplicate), and error.
	SaveEventAndDelivery(ctx context.Context, event *WebhookEvent, delivery *WebhookDelivery) (*WebhookEvent, *WebhookDelivery, bool, error)
	GetWebhookEventByID(ctx context.Context, id string) (*WebhookEvent, error)
	ListWebhookEvents(ctx context.Context, filter EventFilter) ([]*WebhookEvent, int64, error)
	UpdateWebhookEventRouting(ctx context.Context, eventID string, status DeliveryStatus, companyID, routeID *string, delivery *WebhookDelivery) (*WebhookDelivery, error)

	// Deliveries & Worker Execution
	GetDeliveryByID(ctx context.Context, id string) (*WebhookDelivery, error)
	ListDeliveries(ctx context.Context, filter DeliveryFilter) ([]*WebhookDelivery, int64, error)
	ListDeliveriesByEventID(ctx context.Context, eventID string) ([]*WebhookDelivery, error)
	ListAttemptsByEventID(ctx context.Context, eventID string) ([]*DeliveryAttempt, error)
	ClaimDueDeliveries(ctx context.Context, limit int, claimHorizon time.Time) ([]*WebhookDelivery, error)
	RecordDeliveryAttempt(ctx context.Context, delivery *WebhookDelivery, attempt *DeliveryAttempt) error
	ResetDeliveryForRetry(ctx context.Context, deliveryID string) (*WebhookDelivery, error)

	// Stats
	GetGatewayStats(ctx context.Context) (*GatewayStats, error)
}
