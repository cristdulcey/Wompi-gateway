package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/loducode/wompi-gateway/internal/dispatcher"
	"github.com/loducode/wompi-gateway/internal/domain"
	"github.com/loducode/wompi-gateway/internal/repository/memory"
)

func buildSignedWompiWebhook(txID, reference, status string, amount int64, ts int64, secret string) ([]byte, string) {
	concat := fmt.Sprintf("%s%s%d%d%s", txID, status, amount, ts, secret)
	sum := sha256.Sum256([]byte(concat))
	checksum := hex.EncodeToString(sum[:])

	payload := fmt.Sprintf(`{
  "event": "transaction.updated",
  "data": {
    "transaction": {
      "id": %q,
      "amount_in_cents": %d,
      "reference": %q,
      "currency": "COP",
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
  "timestamp": %d
}`, txID, amount, reference, status, checksum, ts)

	return []byte(payload), checksum
}

func TestEndToEnd_MultiCompanyRoutingAndAdminAPI(t *testing.T) {
	ctx := context.Background()
	repo := memory.New()
	disp := dispatcher.New(repo, dispatcher.Config{
		Concurrency:    4,
		PollInterval:   time.Second,
		DefaultTimeout: 5 * time.Second,
	})
	adminKey := "test-super-secret-admin-key"
	router := NewRouter(repo, disp, adminKey, "test")

	var mu sync.Mutex
	var optimaxPayloads []string
	var mrWaitersPayloads []string

	optimaxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		optimaxPayloads = append(optimaxPayloads, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"company":"optimax","received":true}`))
	}))
	defer optimaxServer.Close()

	mrWaitersServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		mrWaitersPayloads = append(mrWaitersPayloads, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"company":"mr-waiters","received":true}`))
	}))
	defer mrWaitersServer.Close()

	// 1. Verify Admin API rejects unauthorized requests
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	unauthRec := httptest.NewRecorder()
	router.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing API key, got %d", unauthRec.Code)
	}

	// Helper for Admin API calls
	adminDo := func(method, path string, body any) *httptest.ResponseRecorder {
		var bodyReader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", adminKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 2. Create Optimax and Mr. Waiters companies
	recOpt := adminDo(http.MethodPost, "/api/v1/companies", map[string]any{
		"name": "Optimax",
		"slug": "optimax",
	})
	if recOpt.Code != http.StatusCreated {
		t.Fatalf("failed to create Optimax company: %d %s", recOpt.Code, recOpt.Body.String())
	}
	var compOptimax domain.Company
	_ = json.Unmarshal(recOpt.Body.Bytes(), &compOptimax)

	recMW := adminDo(http.MethodPost, "/api/v1/companies", map[string]any{
		"name": "Mr. Waiters",
		"slug": "mr-waiters",
	})
	if recMW.Code != http.StatusCreated {
		t.Fatalf("failed to create Mr. Waiters company: %d %s", recMW.Code, recMW.Body.String())
	}
	var compMW domain.Company
	_ = json.Unmarshal(recMW.Body.Bytes(), &compMW)

	// 3. Create shared Wompi account
	wompiSecret := "prod_events_shared_secret_999"
	recAcc := adminDo(http.MethodPost, "/api/v1/accounts", map[string]any{
		"name":          "Wompi Main Prod",
		"slug":          "main-prod",
		"environment":   "prod",
		"events_secret": wompiSecret,
	})
	if recAcc.Code != http.StatusCreated {
		t.Fatalf("failed to create Wompi account: %d %s", recAcc.Code, recAcc.Body.String())
	}
	var acc domain.WompiAccount
	_ = json.Unmarshal(recAcc.Body.Bytes(), &acc)
	if acc.EventsSecret != "" {
		t.Errorf("expected events_secret to be redacted in HTTP response")
	}

	// 4. Create routing rules: sub- -> Optimax, mw-sub- -> Mr. Waiters
	recRoute1 := adminDo(http.MethodPost, "/api/v1/routes", map[string]any{
		"wompi_account_id": acc.ID,
		"company_id":       compOptimax.ID,
		"name":             "Optimax Subscriptions",
		"reference_prefix": "sub-",
		"target_url":       optimaxServer.URL,
		"priority":         10,
	})
	if recRoute1.Code != http.StatusCreated {
		t.Fatalf("failed to create Optimax route: %d %s", recRoute1.Code, recRoute1.Body.String())
	}

	recRoute2 := adminDo(http.MethodPost, "/api/v1/routes", map[string]any{
		"wompi_account_id": acc.ID,
		"company_id":       compMW.ID,
		"name":             "Mr. Waiters Subscriptions",
		"reference_prefix": "mw-sub-",
		"target_url":       mrWaitersServer.URL,
		"priority":         10,
	})
	if recRoute2.Code != http.StatusCreated {
		t.Fatalf("failed to create Mr. Waiters route: %d %s", recRoute2.Code, recRoute2.Body.String())
	}

	// 5. Send Wompi webhook for Optimax (sub-1001)
	optPayload, optChecksum := buildSignedWompiWebhook("tx-opt-1", "sub-1001", "APPROVED", 8900000, 1700001000, wompiSecret)
	whReq1 := httptest.NewRequest(http.MethodPost, "/webhooks/wompi/main-prod", bytes.NewReader(optPayload))
	whReq1.Header.Set("Content-Type", "application/json")
	whReq1.Header.Set("X-Event-Checksum", optChecksum)
	whRec1 := httptest.NewRecorder()
	router.ServeHTTP(whRec1, whReq1)
	if whRec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for Optimax webhook, got %d: %s", whRec1.Code, whRec1.Body.String())
	}

	// Send duplicate webhook and verify idempotency
	whReq1Dup := httptest.NewRequest(http.MethodPost, "/webhooks/wompi/main-prod", bytes.NewReader(optPayload))
	whReq1Dup.Header.Set("Content-Type", "application/json")
	whReq1Dup.Header.Set("X-Event-Checksum", optChecksum)
	whRec1Dup := httptest.NewRecorder()
	router.ServeHTTP(whRec1Dup, whReq1Dup)
	if whRec1Dup.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for duplicate webhook, got %d", whRec1Dup.Code)
	}
	var dupResp map[string]any
	_ = json.Unmarshal(whRec1Dup.Body.Bytes(), &dupResp)
	if dupResp["duplicate"] != true {
		t.Errorf("expected duplicate=true on second identical webhook, got %v", dupResp["duplicate"])
	}

	// 6. Send Wompi webhook for Mr. Waiters (mw-sub-2002)
	mwPayload, mwChecksum := buildSignedWompiWebhook("tx-mw-2", "mw-sub-2002", "APPROVED", 12900000, 1700002000, wompiSecret)
	whReq2 := httptest.NewRequest(http.MethodPost, "/webhooks/wompi/main-prod", bytes.NewReader(mwPayload))
	whReq2.Header.Set("Content-Type", "application/json")
	whReq2.Header.Set("X-Event-Checksum", mwChecksum)
	whRec2 := httptest.NewRecorder()
	router.ServeHTTP(whRec2, whReq2)
	if whRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for Mr. Waiters webhook, got %d: %s", whRec2.Code, whRec2.Body.String())
	}

	// 7. Process due deliveries via Dispatcher
	disp.ProcessDueBatch(ctx)

	mu.Lock()
	if len(optimaxPayloads) != 1 || optimaxPayloads[0] != string(optPayload) {
		t.Errorf("expected Optimax to receive exact raw payload once, got %d payloads", len(optimaxPayloads))
	}
	if len(mrWaitersPayloads) != 1 || mrWaitersPayloads[0] != string(mwPayload) {
		t.Errorf("expected Mr. Waiters to receive exact raw payload once, got %d payloads", len(mrWaitersPayloads))
	}
	mu.Unlock()

	// 8. Test unrouted event and subsequent manual re-route
	unroutedPayload, unroutedChecksum := buildSignedWompiWebhook("tx-unrouted-3", "newco-sub-3003", "APPROVED", 5000000, 1700003000, wompiSecret)
	whReq3 := httptest.NewRequest(http.MethodPost, "/webhooks/wompi/main-prod", bytes.NewReader(unroutedPayload))
	whReq3.Header.Set("Content-Type", "application/json")
	whReq3.Header.Set("X-Event-Checksum", unroutedChecksum)
	whRec3 := httptest.NewRecorder()
	router.ServeHTTP(whRec3, whReq3)
	if whRec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for unrouted webhook, got %d", whRec3.Code)
	}
	var unroutedResp map[string]any
	_ = json.Unmarshal(whRec3.Body.Bytes(), &unroutedResp)
	if unroutedResp["routing"] != string(domain.StatusUnrouted) {
		t.Fatalf("expected routing=unrouted, got %v", unroutedResp["routing"])
	}
	unroutedEventID := unroutedResp["event_id"].(string)

	// Add route rule for newco-sub- -> Optimax and re-route the event
	adminDo(http.MethodPost, "/api/v1/routes", map[string]any{
		"wompi_account_id": acc.ID,
		"company_id":       compOptimax.ID,
		"name":             "NewCo Route",
		"reference_prefix": "newco-sub-",
		"target_url":       optimaxServer.URL,
		"priority":         5,
	})

	rerouteRec := adminDo(http.MethodPost, "/api/v1/events/"+unroutedEventID+"/route", nil)
	if rerouteRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when re-routing event, got %d: %s", rerouteRec.Code, rerouteRec.Body.String())
	}

	disp.ProcessDueBatch(ctx)

	// 9. Verify stats reflect 3 delivered events
	statsRec := adminDo(http.MethodGet, "/api/v1/stats", nil)
	var stats domain.GatewayStats
	_ = json.Unmarshal(statsRec.Body.Bytes(), &stats)
	if stats.TotalEvents != 3 || stats.DeliveredCount != 3 || stats.UnroutedEvents != 0 {
		t.Errorf("expected 3 total events and 3 delivered, got %+v", stats)
	}
}

func TestAdminAuth_HttpOnlySessionCookie(t *testing.T) {
	repo := memory.New()
	disp := dispatcher.New(repo, dispatcher.Config{})
	adminKey := "test-super-secret-admin-key"
	router := NewRouter(repo, disp, adminKey, "test")

	// 1. Login with wrong key returns 401
	badLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"api_key":"wrong"}`)))
	badLoginReq.Header.Set("Content-Type", "application/json")
	badLoginRec := httptest.NewRecorder()
	router.ServeHTTP(badLoginRec, badLoginReq)
	if badLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad login, got %d", badLoginRec.Code)
	}

	// 2. Login with valid key sets HttpOnly + SameSite=Strict cookie
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"api_key":"`+adminKey+`"}`)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	cookies := loginRec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == AdminSessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie to be set", AdminSessionCookieName)
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("expected session cookie to be HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("expected session cookie SameSite=Strict, got %v", sessionCookie.SameSite)
	}
	if sessionCookie.Value == adminKey {
		t.Errorf("expected session cookie value to be an HMAC token, not the raw API key")
	}

	// 3. Call /api/v1/stats using ONLY the HttpOnly cookie (no X-API-Key header)
	statsReq := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	statsReq.AddCookie(sessionCookie)
	statsRec := httptest.NewRecorder()
	router.ServeHTTP(statsRec, statsReq)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with HttpOnly session cookie, got %d: %s", statsRec.Code, statsRec.Body.String())
	}

	// 4. Logout clears the cookie
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRec := httptest.NewRecorder()
	router.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on logout, got %d", logoutRec.Code)
	}
	var clearedCookie *http.Cookie
	for _, c := range logoutRec.Result().Cookies() {
		if c.Name == AdminSessionCookieName {
			clearedCookie = c
			break
		}
	}
	if clearedCookie == nil || clearedCookie.MaxAge >= 0 {
		t.Errorf("expected logout to clear cookie with MaxAge < 0, got %+v", clearedCookie)
	}
}

func TestUpdateEntities_CompaniesAccountsAndRoutes(t *testing.T) {
	repo := memory.New()
	disp := dispatcher.New(repo, dispatcher.Config{})
	adminKey := "test-super-secret-admin-key"
	router := NewRouter(repo, disp, adminKey, "test")

	adminDo := func(method, path string, body any) *httptest.ResponseRecorder {
		var bodyReader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", adminKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Create and Update Company
	createCompRec := adminDo(http.MethodPost, "/api/v1/companies", map[string]any{
		"name":        "Optimax",
		"slug":        "optimax",
		"description": "Initial description",
	})
	if createCompRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for company, got %d", createCompRec.Code)
	}
	var comp domain.Company
	_ = json.Unmarshal(createCompRec.Body.Bytes(), &comp)

	updateCompRec := adminDo(http.MethodPatch, "/api/v1/companies/"+comp.ID, map[string]any{
		"name":        "Optimax ERP",
		"slug":        "optimax-erp",
		"description": "Updated description",
		"active":      false,
	})
	if updateCompRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when updating company, got %d: %s", updateCompRec.Code, updateCompRec.Body.String())
	}
	var updatedComp domain.Company
	_ = json.Unmarshal(updateCompRec.Body.Bytes(), &updatedComp)
	if updatedComp.Name != "Optimax ERP" || updatedComp.Slug != "optimax-erp" || updatedComp.Active != false {
		t.Errorf("unexpected updated company: %+v", updatedComp)
	}

	// 2. Create and Update Wompi Account (preserving secret when omitted)
	createAccRec := adminDo(http.MethodPost, "/api/v1/accounts", map[string]any{
		"name":          "Wompi Prod",
		"slug":          "wompi-prod",
		"environment":   "prod",
		"events_secret": "secret_123",
	})
	if createAccRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for account, got %d", createAccRec.Code)
	}
	var acc domain.WompiAccount
	_ = json.Unmarshal(createAccRec.Body.Bytes(), &acc)

	updateAccRec := adminDo(http.MethodPatch, "/api/v1/accounts/"+acc.ID, map[string]any{
		"name":        "Wompi Sandbox",
		"slug":        "wompi-sandbox",
		"environment": "test",
		"active":      true,
	})
	if updateAccRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when updating account without secret, got %d: %s", updateAccRec.Code, updateAccRec.Body.String())
	}
	storedAcc, err := repo.GetWompiAccountByID(context.Background(), acc.ID)
	if err != nil || storedAcc.EventsSecret != "secret_123" || storedAcc.Slug != "wompi-sandbox" {
		t.Errorf("expected events_secret to be preserved and slug updated, got %+v (err=%v)", storedAcc, err)
	}

	// 3. Create and Update Route Rule
	createRouteRec := adminDo(http.MethodPost, "/api/v1/routes", map[string]any{
		"wompi_account_id": acc.ID,
		"company_id":       comp.ID,
		"name":             "Optimax Route",
		"reference_prefix": "sub-",
		"target_url":       "https://api.optimax.co/webhooks/wompi",
		"priority":         10,
		"max_retries":      5,
	})
	if createRouteRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for route, got %d: %s", createRouteRec.Code, createRouteRec.Body.String())
	}
	var route domain.RouteRule
	_ = json.Unmarshal(createRouteRec.Body.Bytes(), &route)

	updateRouteRec := adminDo(http.MethodPut, "/api/v1/routes/"+route.ID, map[string]any{
		"name":             "Optimax Updated Route",
		"reference_prefix": "opt-sub-",
		"target_url":       "https://api2.optimax.co/webhooks/wompi",
		"priority":         25,
		"max_retries":      8,
		"timeout_seconds":  30,
		"active":           false,
	})
	if updateRouteRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when updating route, got %d: %s", updateRouteRec.Code, updateRouteRec.Body.String())
	}
	var updatedRoute domain.RouteRule
	_ = json.Unmarshal(updateRouteRec.Body.Bytes(), &updatedRoute)
	if updatedRoute.Name != "Optimax Updated Route" ||
		updatedRoute.ReferencePrefix != "opt-sub-" ||
		updatedRoute.Priority != 25 ||
		updatedRoute.MaxRetries != 8 ||
		updatedRoute.TimeoutSeconds != 30 ||
		updatedRoute.Active != false {
		t.Errorf("unexpected updated route: %+v", updatedRoute)
	}
}
