package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testReadiness struct{ err error }

func (d testReadiness) Ready(context.Context) error { return d.err }

func TestHealthAndReadiness(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		name   string
		db     readiness
		path   string
		status int
	}{
		{name: "liveness does not require database", path: "/healthz", status: http.StatusOK},
		{name: "readiness requires configured database", path: "/readyz", status: http.StatusServiceUnavailable},
		{name: "ready database", db: testReadiness{}, path: "/readyz", status: http.StatusOK},
		{name: "database unavailable", db: testReadiness{err: context.DeadlineExceeded}, path: "/readyz", status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := New(test.db, http.Dir("../../web/dist"), logger)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("request ID header missing")
			}
		})
	}
}

func TestAuthRequiresCSRFAndProvidesBootstrapToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := New(nil, http.Dir("../../web/dist"), logger)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/auth/csrf", nil))
	if response.Code != http.StatusOK || response.Result().Cookies()[0].Name != csrfCookieName {
		t.Fatalf("CSRF bootstrap status/cookie incorrect: status=%d cookies=%v", response.Code, response.Result().Cookies())
	}

	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"businessName":"Example","email":"owner@example.com","password":"long-enough-password"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("registration without CSRF token status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestWorkspacePrivateRoutesRequireAuthentication(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := New(nil, http.Dir("../../web/dist"), logger)
	for _, path := range []string{"/api/catalog/categories", "/api/catalog/products", "/api/clients", "/api/invoices"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusUnauthorized)
		}
	}
}

func TestPublicInvoiceUnavailableWithoutStoreAndNotCached(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := New(nil, http.Dir("../../web/dist"), logger)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/invoices/untrusted-token", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("public invoice status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store, private" {
		t.Fatalf("Cache-Control = %q, want no-store, private", got)
	}
}

func TestProductInputRejectsInvalidPricesAndMissingAvailability(t *testing.T) {
	for _, body := range []string{
		`{"name":"Design","description":"","price":"-1.00","currency":"USD","available":true}`,
		`{"name":"Design","description":"","price":"1.001","currency":"USD","available":true}`,
		`{"name":"Design","description":"","price":"1.00","currency":"USD"}`,
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/catalog/products", strings.NewReader(body))
		_, valid := decodeProduct(response, request)
		if valid || response.Code != http.StatusBadRequest {
			t.Errorf("decodeProduct accepted invalid payload %s (status %d)", body, response.Code)
		}
	}
}

func TestRequestLoggerRedactsPublicInvoiceToken(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler := requestLogger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), logger)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/i/sensitive-token", nil))
	if strings.Contains(output.String(), "sensitive-token") {
		t.Fatal("public invoice token was written to request logs")
	}
	if !strings.Contains(output.String(), "/i/:token") {
		t.Fatal("redacted public invoice route was not logged")
	}
}

func TestRequestLoggerRedactsPublicInvoiceAPIToken(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler := requestLogger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), logger)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/public/invoices/private-bearer-token", nil))
	if strings.Contains(output.String(), "private-bearer-token") {
		t.Fatal("public invoice API token was written to request logs")
	}
	if !strings.Contains(output.String(), "/api/public/invoices/:token") {
		t.Fatal("redacted public invoice API route was not logged")
	}
}
