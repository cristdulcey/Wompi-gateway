package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/loducode/wompi-gateway/internal/dispatcher"
	"github.com/loducode/wompi-gateway/internal/domain"
	"github.com/loducode/wompi-gateway/internal/wompi"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MiB

var slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Handler bundles the repository and dispatcher for HTTP route handlers.
type Handler struct {
	repo       domain.Repository
	dispatcher *dispatcher.Dispatcher
}

// NewHandler creates a new HTTP Handler.
func NewHandler(repo domain.Repository, disp *dispatcher.Dispatcher) *Handler {
	return &Handler{
		repo:       repo,
		dispatcher: disp,
	}
}

// HealthCheck handles GET /healthz.
func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"service":   "wompi-webhook-gateway",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// ReceiveWompiWebhook handles POST /webhooks/wompi/:account_slug.
func (h *Handler) ReceiveWompiWebhook(c *gin.Context) {
	slug := strings.TrimSpace(c.Param("account_slug"))
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_slug is required"})
		return
	}

	account, err := h.repo.GetWompiAccountBySlug(c.Request.Context(), slug)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "wompi account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load wompi account"})
		return
	}
	if !account.Active {
		c.JSON(http.StatusNotFound, gin.H{"error": "wompi account is inactive"})
		return
	}

	rawBody, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodyBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	headerChecksum := c.GetHeader("X-Event-Checksum")
	parsed, err := wompi.ParseAndVerify(rawBody, headerChecksum, account.EventsSecret)
	if err != nil {
		if errors.Is(err, wompi.ErrInvalidJSON) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	rules, err := h.repo.ListActiveRouteRulesByAccount(c.Request.Context(), account.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to evaluate routing rules"})
		return
	}

	matchedRule := dispatcher.MatchRoute(parsed.Reference, rules)

	reqHeaders := make(map[string]string)
	for k, vals := range c.Request.Header {
		if len(vals) > 0 {
			reqHeaders[k] = vals[0]
		}
	}

	eventStatus := domain.StatusUnrouted
	var matchedCompanyID *string
	var matchedRouteID *string
	var delivery *domain.WebhookDelivery

	if matchedRule != nil {
		eventStatus = domain.StatusPending
		compID := matchedRule.CompanyID
		routeID := matchedRule.ID
		matchedCompanyID = &compID
		matchedRouteID = &routeID

		delivery = &domain.WebhookDelivery{
			RouteRuleID:    matchedRule.ID,
			CompanyID:      matchedRule.CompanyID,
			CompanyName:    matchedRule.CompanyName,
			TargetURL:      matchedRule.TargetURL,
			CustomHeaders:  matchedRule.CustomHeaders,
			Status:         domain.StatusPending,
			AttemptCount:   0,
			MaxRetries:     matchedRule.MaxRetries,
			TimeoutSeconds: matchedRule.TimeoutSeconds,
			NextRetryAt:    time.Now().UTC(),
		}
	}

	event := &domain.WebhookEvent{
		WompiAccountID:    account.ID,
		AccountSlug:       account.Slug,
		EventType:         parsed.EventType,
		Environment:       parsed.Environment,
		TransactionID:     parsed.TransactionID,
		Reference:         parsed.Reference,
		TransactionStatus: parsed.TransactionStatus,
		AmountInCents:     parsed.AmountInCents,
		Currency:          parsed.Currency,
		Checksum:          parsed.Checksum,
		SignatureValid:    true,
		EventTimestamp:    parsed.Timestamp,
		RawPayload:        json.RawMessage(parsed.RawPayload),
		Headers:           reqHeaders,
		Status:            eventStatus,
		MatchedCompanyID:  matchedCompanyID,
		MatchedRouteID:    matchedRouteID,
	}

	savedEv, savedDel, created, err := h.repo.SaveEventAndDelivery(c.Request.Context(), event, delivery)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist webhook event"})
		return
	}

	if created && savedDel != nil && h.dispatcher != nil {
		h.dispatcher.Notify()
	}

	var deliveryID string
	if savedDel != nil {
		deliveryID = savedDel.ID
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "accepted",
		"duplicate":   !created,
		"event_id":    savedEv.ID,
		"delivery_id": deliveryID,
		"routing":     savedEv.Status,
		"reference":   savedEv.Reference,
	})
}

// GetStats handles GET /api/v1/stats.
func (h *Handler) GetStats(c *gin.Context) {
	stats, err := h.repo.GetGatewayStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load gateway stats"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

type createCompanyReq struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Active      *bool  `json:"active"`
}

// ListCompanies handles GET /api/v1/companies.
func (h *Handler) ListCompanies(c *gin.Context) {
	companies, err := h.repo.ListCompanies(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list companies"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": companies})
}

// CreateCompany handles POST /api/v1/companies.
func (h *Handler) CreateCompany(c *gin.Context) {
	var req createCompanyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	name := strings.TrimSpace(req.Name)
	slug := normalizeSlug(req.Slug, name)
	if name == "" || !slugRegex.MatchString(slug) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid name and alphanumeric hyphenated slug are required"})
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	comp := &domain.Company{
		Name:        name,
		Slug:        slug,
		Description: strings.TrimSpace(req.Description),
		Active:      active,
	}

	if err := h.repo.CreateCompany(c.Request.Context(), comp); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create company"})
		return
	}
	c.JSON(http.StatusCreated, comp)
}

type updateCompanyReq struct {
	Name        *string `json:"name"`
	Slug        *string `json:"slug"`
	Description *string `json:"description"`
	Active      *bool   `json:"active"`
}

// UpdateCompany handles PATCH /api/v1/companies/:id.
func (h *Handler) UpdateCompany(c *gin.Context) {
	id := c.Param("id")
	comp, err := h.repo.GetCompanyByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "company not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch company"})
		return
	}

	var req updateCompanyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if req.Name != nil {
		comp.Name = strings.TrimSpace(*req.Name)
	}
	if req.Slug != nil {
		comp.Slug = strings.ToLower(strings.TrimSpace(*req.Slug))
	}
	if req.Description != nil {
		comp.Description = strings.TrimSpace(*req.Description)
	}
	if req.Active != nil {
		comp.Active = *req.Active
	}

	if comp.Name == "" || !slugRegex.MatchString(comp.Slug) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid name and slug are required"})
		return
	}

	if err := h.repo.UpdateCompany(c.Request.Context(), comp); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update company"})
		return
	}
	c.JSON(http.StatusOK, comp)
}

type createAccountReq struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Environment  string `json:"environment"`
	EventsSecret string `json:"events_secret"`
	Active       *bool  `json:"active"`
}

// ListAccounts handles GET /api/v1/accounts.
func (h *Handler) ListAccounts(c *gin.Context) {
	accounts, err := h.repo.ListWompiAccounts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list wompi accounts"})
		return
	}
	for _, a := range accounts {
		a.EventsSecret = ""
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

// CreateAccount handles POST /api/v1/accounts.
func (h *Handler) CreateAccount(c *gin.Context) {
	var req createAccountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	name := strings.TrimSpace(req.Name)
	slug := normalizeSlug(req.Slug, name)
	secret := strings.TrimSpace(req.EventsSecret)
	env := strings.ToLower(strings.TrimSpace(req.Environment))
	if env == "" {
		env = "prod"
	}

	if name == "" || !slugRegex.MatchString(slug) || secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, valid slug, and events_secret are required"})
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}

	acc := &domain.WompiAccount{
		Name:         name,
		Slug:         slug,
		Environment:  env,
		EventsSecret: secret,
		Active:       active,
	}

	if err := h.repo.CreateWompiAccount(c.Request.Context(), acc); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create wompi account"})
		return
	}

	acc.EventsSecret = ""
	c.JSON(http.StatusCreated, acc)
}

type updateAccountReq struct {
	Name         *string `json:"name"`
	Slug         *string `json:"slug"`
	Environment  *string `json:"environment"`
	EventsSecret *string `json:"events_secret"`
	Active       *bool   `json:"active"`
}

// UpdateAccount handles PATCH /api/v1/accounts/:id.
func (h *Handler) UpdateAccount(c *gin.Context) {
	id := c.Param("id")
	acc, err := h.repo.GetWompiAccountByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "wompi account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch wompi account"})
		return
	}

	var req updateAccountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if req.Name != nil {
		acc.Name = strings.TrimSpace(*req.Name)
	}
	if req.Slug != nil {
		acc.Slug = strings.ToLower(strings.TrimSpace(*req.Slug))
	}
	if req.Environment != nil {
		acc.Environment = strings.ToLower(strings.TrimSpace(*req.Environment))
	}
	if req.EventsSecret != nil && strings.TrimSpace(*req.EventsSecret) != "" {
		acc.EventsSecret = strings.TrimSpace(*req.EventsSecret)
	}
	if req.Active != nil {
		acc.Active = *req.Active
	}

	if acc.Name == "" || !slugRegex.MatchString(acc.Slug) || acc.EventsSecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid name, slug, and events_secret are required"})
		return
	}

	if err := h.repo.UpdateWompiAccount(c.Request.Context(), acc); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update wompi account"})
		return
	}

	acc.EventsSecret = ""
	c.JSON(http.StatusOK, acc)
}

type createRouteReq struct {
	WompiAccountID  string            `json:"wompi_account_id"`
	CompanyID       string            `json:"company_id"`
	Name            string            `json:"name"`
	ReferencePrefix string            `json:"reference_prefix"`
	TargetURL       string            `json:"target_url"`
	CustomHeaders   map[string]string `json:"custom_headers"`
	MaxRetries      int               `json:"max_retries"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	Priority        int               `json:"priority"`
	Active          *bool             `json:"active"`
}

// ListRoutes handles GET /api/v1/routes.
func (h *Handler) ListRoutes(c *gin.Context) {
	accountID := strings.TrimSpace(c.Query("account_id"))
	routes, err := h.repo.ListRouteRules(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list route rules"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": routes})
}

// CreateRoute handles POST /api/v1/routes.
func (h *Handler) CreateRoute(c *gin.Context) {
	var req createRouteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if strings.TrimSpace(req.WompiAccountID) == "" ||
		strings.TrimSpace(req.CompanyID) == "" ||
		strings.TrimSpace(req.Name) == "" ||
		!isValidHTTPURL(req.TargetURL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wompi_account_id, company_id, name, and valid http/https target_url are required"})
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}
	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}
	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 15
	}

	rule := &domain.RouteRule{
		WompiAccountID:  strings.TrimSpace(req.WompiAccountID),
		CompanyID:       strings.TrimSpace(req.CompanyID),
		Name:            strings.TrimSpace(req.Name),
		ReferencePrefix: strings.TrimSpace(req.ReferencePrefix),
		TargetURL:       strings.TrimSpace(req.TargetURL),
		CustomHeaders:   req.CustomHeaders,
		MaxRetries:      maxRetries,
		TimeoutSeconds:  timeoutSeconds,
		Priority:        req.Priority,
		Active:          active,
	}

	if err := h.repo.CreateRouteRule(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

type updateRouteReq struct {
	WompiAccountID  *string           `json:"wompi_account_id"`
	CompanyID       *string           `json:"company_id"`
	Name            *string           `json:"name"`
	ReferencePrefix *string           `json:"reference_prefix"`
	TargetURL       *string           `json:"target_url"`
	CustomHeaders   map[string]string `json:"custom_headers"`
	MaxRetries      *int              `json:"max_retries"`
	TimeoutSeconds  *int              `json:"timeout_seconds"`
	Priority        *int              `json:"priority"`
	Active          *bool             `json:"active"`
}

// UpdateRoute handles PATCH /api/v1/routes/:id.
func (h *Handler) UpdateRoute(c *gin.Context) {
	id := c.Param("id")
	rule, err := h.repo.GetRouteRuleByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "route rule not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load route rule"})
		return
	}

	var req updateRouteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if req.WompiAccountID != nil {
		rule.WompiAccountID = strings.TrimSpace(*req.WompiAccountID)
	}
	if req.CompanyID != nil {
		rule.CompanyID = strings.TrimSpace(*req.CompanyID)
	}
	if req.Name != nil {
		rule.Name = strings.TrimSpace(*req.Name)
	}
	if req.ReferencePrefix != nil {
		rule.ReferencePrefix = strings.TrimSpace(*req.ReferencePrefix)
	}
	if req.TargetURL != nil {
		rule.TargetURL = strings.TrimSpace(*req.TargetURL)
	}
	if req.CustomHeaders != nil {
		rule.CustomHeaders = req.CustomHeaders
	}
	if req.MaxRetries != nil && *req.MaxRetries > 0 {
		rule.MaxRetries = *req.MaxRetries
	}
	if req.TimeoutSeconds != nil && *req.TimeoutSeconds > 0 {
		rule.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.Priority != nil {
		rule.Priority = *req.Priority
	}
	if req.Active != nil {
		rule.Active = *req.Active
	}

	if rule.Name == "" || !isValidHTTPURL(rule.TargetURL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid name and http/https target_url are required"})
		return
	}

	if err := h.repo.UpdateRouteRule(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// DeleteRoute handles DELETE /api/v1/routes/:id.
func (h *Handler) DeleteRoute(c *gin.Context) {
	id := c.Param("id")
	if err := h.repo.DeleteRouteRule(c.Request.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "route rule not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete route rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ListEvents handles GET /api/v1/events.
func (h *Handler) ListEvents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	filter := domain.EventFilter{
		AccountID: strings.TrimSpace(c.Query("account_id")),
		CompanyID: strings.TrimSpace(c.Query("company_id")),
		Reference: strings.TrimSpace(c.Query("reference")),
		Status:    strings.TrimSpace(c.Query("status")),
		Limit:     limit,
		Offset:    offset,
	}

	events, total, err := h.repo.ListWebhookEvents(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list webhook events"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":   events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetEvent handles GET /api/v1/events/:id.
func (h *Handler) GetEvent(c *gin.Context) {
	id := c.Param("id")
	ev, err := h.repo.GetWebhookEventByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook event not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch webhook event"})
		return
	}

	deliveries, err := h.repo.ListDeliveriesByEventID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch event deliveries"})
		return
	}

	attempts, err := h.repo.ListAttemptsByEventID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch delivery attempts"})
		return
	}

	c.JSON(http.StatusOK, domain.EventDetail{
		Event:      ev,
		Deliveries: deliveries,
		Attempts:   attempts,
	})
}

// RouteUnroutedEvent handles POST /api/v1/events/:id/route.
// It re-evaluates active routing rules for an event and queues a new delivery if a match is found.
func (h *Handler) RouteUnroutedEvent(c *gin.Context) {
	id := c.Param("id")
	ev, err := h.repo.GetWebhookEventByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook event not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch webhook event"})
		return
	}

	rules, err := h.repo.ListActiveRouteRulesByAccount(c.Request.Context(), ev.WompiAccountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load active route rules"})
		return
	}

	matched := dispatcher.MatchRoute(ev.Reference, rules)
	if matched == nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":     "no active route rule matches reference",
			"reference": ev.Reference,
		})
		return
	}

	del := &domain.WebhookDelivery{
		RouteRuleID:    matched.ID,
		CompanyID:      matched.CompanyID,
		CompanyName:    matched.CompanyName,
		TargetURL:      matched.TargetURL,
		CustomHeaders:  matched.CustomHeaders,
		Status:         domain.StatusPending,
		AttemptCount:   0,
		MaxRetries:     matched.MaxRetries,
		TimeoutSeconds: matched.TimeoutSeconds,
		NextRetryAt:    time.Now().UTC(),
	}

	compID := matched.CompanyID
	routeID := matched.ID
	savedDel, err := h.repo.UpdateWebhookEventRouting(c.Request.Context(), ev.ID, domain.StatusPending, &compID, &routeID, del)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create delivery for event"})
		return
	}

	if h.dispatcher != nil {
		h.dispatcher.Notify()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "queued",
		"delivery": savedDel,
	})
}

// ListDeliveries handles GET /api/v1/deliveries.
func (h *Handler) ListDeliveries(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	filter := domain.DeliveryFilter{
		AccountID: strings.TrimSpace(c.Query("account_id")),
		CompanyID: strings.TrimSpace(c.Query("company_id")),
		Status:    strings.TrimSpace(c.Query("status")),
		Limit:     limit,
		Offset:    offset,
	}

	deliveries, total, err := h.repo.ListDeliveries(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deliveries"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":   deliveries,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// RetryDelivery handles POST /api/v1/deliveries/:id/retry.
func (h *Handler) RetryDelivery(c *gin.Context) {
	id := c.Param("id")
	del, err := h.repo.ResetDeliveryForRetry(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "delivery not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset delivery for retry"})
		return
	}

	if h.dispatcher != nil {
		h.dispatcher.Notify()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "queued",
		"delivery": del,
	})
}

func normalizeSlug(slug, fallbackName string) string {
	s := strings.ToLower(strings.TrimSpace(slug))
	if s == "" {
		s = strings.ToLower(strings.TrimSpace(fallbackName))
		s = strings.ReplaceAll(s, " ", "-")
		s = strings.ReplaceAll(s, "_", "-")
		s = strings.ReplaceAll(s, ".", "")
	}
	return s
}

func isValidHTTPURL(raw string) bool {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
