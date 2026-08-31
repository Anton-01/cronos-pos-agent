package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testToken  = "11111111-2222-3333-4444-555555555555"
	testOrigin = "https://pos.cronos.test"
)

func testRouter() http.Handler {
	// The uptime of the health payload is measured against this, and a zero
	// value would report the seconds elapsed since year 1.
	startTime = time.Now()

	return NewRouter(Config{
		APIToken:       testToken,
		AllowedOrigins: []string{testOrigin},
		Port:           9100,
	})
}

// The frontend pings the agent before it has any token: discovery must not be
// answered with a 401.
func TestPublicRoutesAnswerWithoutToken(t *testing.T) {
	router := testRouter()

	for _, path := range []string{"/health", "/api/health"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s without token: got %d, want %d", path, rec.Code, http.StatusOK)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s: response is not valid JSON: %v", path, err)
		}
		if body["status"] != "ok" {
			t.Errorf("GET %s: got status %v, want \"ok\"", path, body["status"])
		}
		if body["version"] != AgentVersion {
			t.Errorf("GET %s: got version %v, want %q", path, body["version"], AgentVersion)
		}
	}
}

func TestProtectedRoutesRejectMissingOrWrongToken(t *testing.T) {
	router := testRouter()

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/printers"},
		{http.MethodGet, "/api/printers/queue?printer_name=POS-80"},
		{http.MethodPost, "/api/print"},
		{http.MethodPost, "/api/print/pdf"},
		// Not registered anywhere: the /api/ subtree is fail-closed, so an
		// endpoint added tomorrow is guarded even before it exists.
		{http.MethodGet, "/api/whatever"},
	}

	tokens := map[string]string{
		"missing token": "",
		"wrong token":   "not-the-token",
	}

	for name, token := range tokens {
		for _, tc := range paths {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if token != "" {
				req.Header.Set("X-Cronos-Agent-Token", token)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with %s: got %d, want %d", tc.method, tc.path, name, rec.Code, http.StatusUnauthorized)
			}
		}
	}
}

// A valid token must get past the middleware. The requests below use the wrong
// HTTP method on purpose: a 405 proves the handler was reached without sending
// anything to a real printer.
func TestProtectedRoutesAcceptValidToken(t *testing.T) {
	router := testRouter()

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/printers"},
		{http.MethodPost, "/api/printers/queue"},
		{http.MethodGet, "/api/print"},
		{http.MethodGet, "/api/print/pdf"},
	}

	for _, tc := range paths {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-Cronos-Agent-Token", testToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s with a valid token: got %d, want %d", tc.method, tc.path, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}

// Without the CORS headers the browser drops the discovery response before the
// frontend can read it, so the public routes need them just as much as the
// protected ones.
func TestCORSWrapsPublicAndProtectedRoutes(t *testing.T) {
	router := testRouter()

	for _, path := range []string{"/health", "/api/health", "/api/printers"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", testOrigin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
			t.Errorf("GET %s: got Access-Control-Allow-Origin %q, want %q", path, got, testOrigin)
		}
	}
}

func TestCORSPreflightOnPublicHealth(t *testing.T) {
	router := testRouter()

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight on /api/health: got %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("preflight on /api/health: Access-Control-Allow-Headers is empty")
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	router := testRouter()

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://attacker.test")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("preflight from an unlisted origin: got %d, want %d", rec.Code, http.StatusForbidden)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("preflight from an unlisted origin: got Access-Control-Allow-Origin %q, want it empty", got)
	}
}
