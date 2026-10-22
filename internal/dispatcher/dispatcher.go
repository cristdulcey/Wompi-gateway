package dispatcher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/loducode/wompi-gateway/internal/domain"
)

const maxResponseBodyLogBytes = 4096

// DefaultBackoffSchedule defines the base retry intervals for failed delivery attempts.
var DefaultBackoffSchedule = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
}

// BackoffFunc calculates the delay before the next retry for the given 1-based attempt number.
type BackoffFunc func(attempt int) time.Duration

// DefaultBackoff returns the exponential backoff interval with +/- 10% jitter for a 1-based attempt count.
func DefaultBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	idx := attempt - 1
	if idx >= len(DefaultBackoffSchedule) {
		idx = len(DefaultBackoffSchedule) - 1
	}
	base := DefaultBackoffSchedule[idx]
	// Apply +/- 10% jitter
	jitterFactor := 0.9 + (rand.Float64() * 0.2)
	return time.Duration(float64(base) * jitterFactor)
}

// Dispatcher claims pending/retrying webhook deliveries and forwards raw payloads to target endpoints.
type Dispatcher struct {
	repo           domain.Repository
	httpClient     *http.Client
	concurrency    int
	pollInterval   time.Duration
	defaultTimeout time.Duration
	backoff        BackoffFunc
	notifyCh       chan struct{}
	logger         *slog.Logger
}

// Config configures the Dispatcher worker engine.
type Config struct {
	Concurrency    int
	PollInterval   time.Duration
	DefaultTimeout time.Duration
	Backoff        BackoffFunc
	HTTPClient     *http.Client
	Logger         *slog.Logger
}

// New creates a new Dispatcher instance.
func New(repo domain.Repository, cfg Config) *Dispatcher {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 15 * time.Second
	}
	if cfg.Backoff == nil {
		cfg.Backoff = DefaultBackoff
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: cfg.DefaultTimeout,
		}
	}

	return &Dispatcher{
		repo:           repo,
		httpClient:     client,
		concurrency:    cfg.Concurrency,
		pollInterval:   cfg.PollInterval,
		defaultTimeout: cfg.DefaultTimeout,
		backoff:        cfg.Backoff,
		notifyCh:       make(chan struct{}, 1),
		logger:         cfg.Logger,
	}
}

// Notify triggers an immediate non-blocking dispatch cycle.
func (d *Dispatcher) Notify() {
	select {
	case d.notifyCh <- struct{}{}:
	default:
	}
}

// Start runs the background polling and notification loop until ctx is canceled.
func (d *Dispatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	d.logger.Info("webhook dispatcher started",
		"concurrency", d.concurrency,
		"poll_interval", d.pollInterval.String(),
	)

	// Perform an initial sweep on startup.
	d.ProcessDueBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("webhook dispatcher shutting down")
			return
		case <-d.notifyCh:
			d.ProcessDueBatch(ctx)
		case <-ticker.C:
			d.ProcessDueBatch(ctx)
		}
	}
}

// ProcessDueBatch claims due deliveries up to batch capacity and dispatches them concurrently.
func (d *Dispatcher) ProcessDueBatch(ctx context.Context) int {
	if ctx.Err() != nil {
		return 0
	}

	// While a delivery is being processed, push its NextRetryAt forward by a safety lease
	// so concurrent workers do not double-claim it if HTTP takes several seconds.
	leaseHorizon := time.Now().UTC().Add(2 * time.Minute)
	batchSize := d.concurrency * 4
	if batchSize < 10 {
		batchSize = 10
	}

	deliveries, err := d.repo.ClaimDueDeliveries(ctx, batchSize, leaseHorizon)
	if err != nil {
		d.logger.Error("failed to claim due deliveries", "error", err)
		return 0
	}
	if len(deliveries) == 0 {
		return 0
	}

	sem := make(chan struct{}, d.concurrency)
	var wg sync.WaitGroup

	for _, del := range deliveries {
		wg.Add(1)
		sem <- struct{}{}
		go func(delivery *domain.WebhookDelivery) {
			defer wg.Done()
			defer func() { <-sem }()
			d.deliverOne(ctx, delivery)
		}(del)
	}

	wg.Wait()
	return len(deliveries)
}

func (d *Dispatcher) deliverOne(ctx context.Context, delivery *domain.WebhookDelivery) {
	timeout := d.defaultTimeout
	if delivery.TimeoutSeconds > 0 {
		timeout = time.Duration(delivery.TimeoutSeconds) * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	attemptNum := delivery.AttemptCount + 1
	start := time.Now()

	var statusCode *int
	var respBodyStr string
	var errMsg string
	var success bool

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, delivery.TargetURL, bytes.NewReader(delivery.RawPayload))
	if err != nil {
		errMsg = fmt.Sprintf("failed to build request: %v", err)
	} else {
		// Apply custom headers first so standard integrity headers cannot be accidentally blanked out,
		// or allow custom headers like Authorization / X-Tenant-Secret.
		for k, v := range delivery.CustomHeaders {
			if k != "" {
				req.Header.Set(k, v)
			}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Wompi-Webhook-Gateway/1.0")
		if delivery.Checksum != "" {
			req.Header.Set("X-Event-Checksum", delivery.Checksum)
		}
		req.Header.Set("X-Wompi-Gateway-Event-ID", delivery.WebhookEventID)
		req.Header.Set("X-Wompi-Gateway-Delivery-ID", delivery.ID)
		req.Header.Set("X-Wompi-Gateway-Attempt", strconv.Itoa(attemptNum))

		resp, doErr := d.httpClient.Do(req)
		if doErr != nil {
			errMsg = doErr.Error()
		} else {
			defer resp.Body.Close()
			code := resp.StatusCode
			statusCode = &code
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyLogBytes))
			respBodyStr = string(bodyBytes)

			if code >= 200 && code < 300 {
				success = true
			} else {
				errMsg = fmt.Sprintf("upstream returned HTTP %d", code)
			}
		}
	}

	durationMs := time.Since(start).Milliseconds()
	now := time.Now().UTC()

	attempt := &domain.DeliveryAttempt{
		ID:             uuid.NewString(),
		DeliveryID:     delivery.ID,
		WebhookEventID: delivery.WebhookEventID,
		AttemptNumber:  attemptNum,
		TargetURL:      delivery.TargetURL,
		StatusCode:     statusCode,
		ResponseBody:   respBodyStr,
		ErrorMessage:   errMsg,
		DurationMs:     durationMs,
		Success:        success,
		CreatedAt:      now,
	}

	delivery.AttemptCount = attemptNum
	delivery.LastStatusCode = statusCode
	delivery.LastError = errMsg
	delivery.LastResponseBody = respBodyStr
	delivery.UpdatedAt = now

	if success {
		delivery.Status = domain.StatusDelivered
		delivery.DeliveredAt = &now
	} else {
		maxRetries := delivery.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 5
		}
		if attemptNum >= maxRetries {
			delivery.Status = domain.StatusDeadLetter
		} else {
			delivery.Status = domain.StatusRetrying
			delivery.NextRetryAt = now.Add(d.backoff(attemptNum))
		}
	}

	// Use a detached context with timeout so we persist attempt outcome even if parent ctx is shutting down.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()

	if err := d.repo.RecordDeliveryAttempt(saveCtx, delivery, attempt); err != nil {
		d.logger.Error("failed to record delivery attempt",
			"delivery_id", delivery.ID,
			"event_id", delivery.WebhookEventID,
			"error", err,
		)
	}
}
