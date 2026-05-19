package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/api"
	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/ingest"
	"github.com/mitigador/mitigador/internal/incident"
	"github.com/mitigador/mitigador/internal/session"
	"github.com/mitigador/mitigador/internal/user"
)

// testDSN returns the Postgres DSN from env; skips the test if absent.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set")
	}
	return dsn
}

// setupServer creates a test server backed by a real Postgres pool.
// Returns the server, the pool, and the user store.
// Delegates to setupServerWithTraffic with nil Store and Catalog.
func setupServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *user.Store) {
	t.Helper()
	srv, pool, us, _, _ := setupServerWithTraffic(t, nil, nil)
	return srv, pool, us
}

// setupServerWithTraffic lets a test inject the aggregate.Store and detect.Catalog.
// Pass nil for either to use the zero defaults (an empty store / no catalog).
func setupServerWithTraffic(t *testing.T, store *aggregate.Store, catalog *detect.Catalog) (*httptest.Server, *pgxpool.Pool, *user.Store, *aggregate.Store, *detect.Catalog) {
	t.Helper()
	dsn := testDSN(t)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("pool.Ping: %v", err)
	}

	sm := session.NewManager(pool, false)
	userStore := user.NewStore(pool)
	incStore := incident.NewStore(pool)
	inv := &ingest.Inventory{}
	health := ingest.NewHealthTracker()
	sseChan := make(chan detect.AttackEvent, 16)
	broker := api.NewBroker(sseChan)

	if store == nil {
		store = aggregate.New(1)
	}

	handler := api.New(api.Deps{
		Pool:      pool,
		SM:        sm,
		Users:     userStore,
		Incidents: incStore,
		Inventory: inv,
		Health:    health,
		SSEBroker: broker,
		Store:     store,
		Catalog:   catalog,
	})
	srv := httptest.NewServer(handler)

	t.Cleanup(func() {
		srv.Close()
		pool.Close()
	})

	return srv, pool, userStore, store, catalog
}

// createTestUser creates a user in the DB and returns username/password.
func createTestUser(t *testing.T, store *user.Store) (string, string) {
	t.Helper()
	username := "testuser_" + strings.ReplaceAll(t.Name(), "/", "_")
	password := "supersecretpassword123"
	ctx := context.Background()
	// clean up any leftover from previous run
	_ = store.Delete(ctx, username)
	_, err := store.Create(ctx, username, "test@example.com", password)
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Delete(context.Background(), username)
	})
	return username, password
}

// login performs POST /api/auth/login and returns the response + cookies.
func login(t *testing.T, srv *httptest.Server, username, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	resp, err := srv.Client().Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	return resp
}

// TestLogin_ValidCredentials_Returns204AndSetsCookie tests happy path login.
func TestLogin_ValidCredentials_Returns204AndSetsCookie(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	resp := login(t, srv, username, password)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	cookieHeader := resp.Header.Get("Set-Cookie")
	if cookieHeader == "" {
		t.Fatal("expected Set-Cookie header, got none")
	}
	if !strings.Contains(cookieHeader, "HttpOnly") {
		t.Errorf("cookie missing HttpOnly: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, "Secure") {
		t.Errorf("cookie missing Secure: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, "SameSite=Lax") {
		t.Errorf("cookie missing SameSite=Lax: %s", cookieHeader)
	}
}

// TestLogin_WrongPassword_Returns401WithUniformError tests wrong password.
func TestLogin_WrongPassword_Returns401WithUniformError(t *testing.T) {
	srv, _, store := setupServer(t)
	username, _ := createTestUser(t, store)

	body, _ := json.Marshal(map[string]string{"Username": username, "Password": "wrong_password"})
	resp, err := srv.Client().Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["error"] != "invalid_credentials" {
		t.Errorf("want error=invalid_credentials, got %q", result["error"])
	}
}

// TestLogin_NonexistentUser_Returns401WithUniformError tests user not found.
func TestLogin_NonexistentUser_Returns401WithUniformError(t *testing.T) {
	srv, _, _ := setupServer(t)

	body, _ := json.Marshal(map[string]string{"Username": "no_such_user_xyz", "Password": "any_password"})
	resp, err := srv.Client().Post(srv.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["error"] != "invalid_credentials" {
		t.Errorf("want error=invalid_credentials, got %q", result["error"])
	}
}

// TestLogin_MalformedJSON_Returns400 tests bad JSON body.
func TestLogin_MalformedJSON_Returns400(t *testing.T) {
	srv, _, _ := setupServer(t)

	resp, err := srv.Client().Post(srv.URL+"/api/auth/login", "application/json", strings.NewReader("{not valid json"))
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// TestLogin_RenewsToken_BeforePuttingUserID checks session fixation defense.
// We verify that the session cookie value after login differs from any pre-existing session.
func TestLogin_RenewsToken_BeforePuttingUserID(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	client := srv.Client()
	jar := newCookieJar(t)
	client.Jar = jar

	// First login — capture session cookie.
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	resp1.Body.Close()
	cookie1 := sessionCookie(jar, srv.URL)

	// Logout so we get a fresh session for the second login.
	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	resp2, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp2.Body.Close()

	// Second login — cookie must differ (RenewToken rotated the token).
	body2, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp3, err := client.Do(req2)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	resp3.Body.Close()
	cookie2 := sessionCookie(jar, srv.URL)

	if cookie1 == "" {
		t.Fatal("no session cookie after first login")
	}
	if cookie2 == "" {
		t.Fatal("no session cookie after second login")
	}
	if cookie1 == cookie2 {
		t.Errorf("session token was NOT renewed — fixation risk: both cookies = %q", cookie1)
	}
}

// TestLogout_DestroysSession verifies session is gone after logout.
func TestLogout_DestroysSession(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	client := srv.Client()
	jar := newCookieJar(t)
	client.Jar = jar

	// Login.
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: want 204, got %d", resp.StatusCode)
	}

	// Verify /me works.
	meResp, _ := client.Get(srv.URL + "/api/auth/me")
	meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("me before logout: want 200, got %d", meResp.StatusCode)
	}

	// Logout.
	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	logoutResp, _ := client.Do(logoutReq)
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d", logoutResp.StatusCode)
	}

	// /me should now 401.
	meResp2, _ := client.Get(srv.URL + "/api/auth/me")
	meResp2.Body.Close()
	if meResp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d", meResp2.StatusCode)
	}
}

// TestMe_AuthenticatedReturnsUserPayload verifies /api/auth/me response payload.
func TestMe_AuthenticatedReturnsUserPayload(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	client := srv.Client()
	jar := newCookieJar(t)
	client.Jar = jar

	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	resp.Body.Close()

	meResp, err := client.Get(srv.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer meResp.Body.Close()

	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", meResp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(meResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if payload["username"] != username {
		t.Errorf("want username=%q, got %v", username, payload["username"])
	}
	if payload["id"] == nil {
		t.Errorf("expected id field in /me response")
	}
}

// TestMe_UnauthenticatedReturns401 verifies /me without a session returns 401.
func TestMe_UnauthenticatedReturns401(t *testing.T) {
	srv, _, _ := setupServer(t)

	resp, err := srv.Client().Get(srv.URL + "/api/auth/me")
	if err != nil {
		t.Fatalf("GET /api/auth/me: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

// TestCSRF_Get_ReturnsToken verifies GET /api/auth/csrf returns a token.
func TestCSRF_Get_ReturnsToken(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	client := srv.Client()
	jar := newCookieJar(t)
	client.Jar = jar

	// login first so we have a session
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	resp.Body.Close()

	csrfResp, err := client.Get(srv.URL + "/api/auth/csrf")
	if err != nil {
		t.Fatalf("GET /api/auth/csrf: %v", err)
	}
	defer csrfResp.Body.Close()

	if csrfResp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", csrfResp.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(csrfResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode csrf response: %v", err)
	}
	if payload["token"] == "" {
		t.Errorf("expected non-empty CSRF token")
	}
}

// TestCSRF_NonGETWithoutToken_Returns403 verifies CSRF protection on non-GET.
func TestCSRF_NonGETWithoutToken_Returns403(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	client := srv.Client()
	jar := newCookieJar(t)
	client.Jar = jar

	// login
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	resp.Body.Close()

	// POST logout WITHOUT csrf token — should 403
	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	// intentionally no X-CSRF-Token header
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST /api/auth/logout: %v", err)
	}
	defer logoutResp.Body.Close()

	if logoutResp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", logoutResp.StatusCode)
	}
}

// TestCSRF_NonGETWithToken_Succeeds verifies that correct CSRF token allows state-changing requests.
func TestCSRF_NonGETWithToken_Succeeds(t *testing.T) {
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	client := srv.Client()
	jar := newCookieJar(t)
	client.Jar = jar

	// login
	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	resp.Body.Close()

	// Get CSRF token
	csrfResp, _ := client.Get(srv.URL + "/api/auth/csrf")
	var csrfPayload map[string]string
	_ = json.NewDecoder(csrfResp.Body).Decode(&csrfPayload)
	csrfResp.Body.Close()
	token := csrfPayload["token"]
	if token == "" {
		t.Fatal("no CSRF token returned")
	}

	// POST logout WITH csrf token — should succeed
	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
	logoutReq.Header.Set("X-CSRF-Token", token)
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST /api/auth/logout: %v", err)
	}
	defer logoutResp.Body.Close()

	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", logoutResp.StatusCode)
	}
}
