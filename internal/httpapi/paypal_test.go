package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/WispHive-Networks/WispPay/internal/paypal"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

type paypalRouteStore struct {
	*mysqlstore.DB
	*memoryIdentityStore
	completed  bool
	stateHash  string
	merchantID string
}

func (s *paypalRouteStore) CompletePayPalSellerOnboarding(_ context.Context, businessID, stateHash, merchantID string, _, _, _ bool) error {
	if businessID != "business-id" {
		return mysqlstore.ErrSellerStateExpired
	}
	s.completed = true
	s.stateHash = stateHash
	s.merchantID = merchantID
	return nil
}

func (s *paypalRouteStore) GetPublicInvoicePayPal(context.Context, string) (mysqlstore.PayPalInvoice, error) {
	return mysqlstore.PayPalInvoice{Status: "open", Currency: "USD", Amount: "10.00", MerchantID: "seller-merchant"}, nil
}

type paypalRouteProvider struct {
	PayPalClient
	status paypal.SellerStatus
}

func (p paypalRouteProvider) GetSellerStatus(context.Context, string, string, string) (paypal.SellerStatus, error) {
	return p.status, nil
}

func TestPayPalOnboardingRequiresPayPalTrackingIDForCurrentBusiness(t *testing.T) {
	state := "0123456789abcdef0123456789abcdef"
	identity := &memoryIdentityStore{sessions: map[string]mysqlstore.SessionIdentity{
		tokenHash("session-secret"): {IdentityUser: mysqlstore.IdentityUser{ID: "user-id", BusinessID: "business-id"}},
	}}
	store := &paypalRouteStore{memoryIdentityStore: identity}
	provider := paypalRouteProvider{status: paypal.SellerStatus{
		TrackingID: "another-business", MerchantID: "seller-merchant", PaymentsReceivable: true,
		EmailConfirmed: true, PaymentPermission: true,
	}}
	mux := http.NewServeMux()
	registerPayPalRoutes(mux, provider, store, identity, PayPalSettings{
		Enabled: true, PartnerMerchantID: "partner", AttributionID: "attribution", WebhookID: "webhook",
		PublicBaseURL: "https://wisppay.example",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/paypal/seller/onboarding/return?state="+url.QueryEscape(state)+"&merchantIdInPayPal=seller-merchant", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("callback status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	if store.completed {
		t.Fatal("seller was activated with a tracking ID belonging to a different business")
	}

	provider.status.TrackingID = "business-id"
	mux = http.NewServeMux()
	registerPayPalRoutes(mux, provider, store, identity, PayPalSettings{
		Enabled: true, PartnerMerchantID: "partner", AttributionID: "attribution", WebhookID: "webhook",
		PublicBaseURL: "https://wisppay.example",
	})
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || !store.completed || store.stateHash != tokenHash(state) || store.merchantID != "seller-merchant" {
		t.Fatalf("valid callback status/body/store = %d %s %+v", response.Code, response.Body.String(), store)
	}
}

func TestPublicPayPalCapabilityIsUnavailableWhenNotConfigured(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := New(nil, http.Dir("../../web/dist"), logger)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/invoices/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef/paypal", nil))
	if response.Code != http.StatusOK || response.Body.String() != "{\"available\":false}\n" {
		t.Fatalf("unconfigured capability = %d %s", response.Code, response.Body.String())
	}
}

func TestServerRegistersConfiguredPayPalCapabilityRoute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := &paypalRouteStore{memoryIdentityStore: &memoryIdentityStore{}}
	handler := New(store, http.Dir("../../web/dist"), logger, PayPalIntegration{
		Client: paypalRouteProvider{},
		Settings: PayPalSettings{
			Enabled: true, PartnerMerchantID: "partner", AttributionID: "attribution", WebhookID: "webhook",
			PublicBaseURL: "https://wisppay.example",
		},
	})
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/invoices/"+token+"/paypal", nil))
	if response.Code != http.StatusOK || response.Body.String() != "{\"available\":true}\n" {
		t.Fatalf("configured capability route = %d %s", response.Code, response.Body.String())
	}
}

func TestPayPalCallbackRejectsMissingAuthentication(t *testing.T) {
	mux := http.NewServeMux()
	registerPayPalRoutes(mux, paypalRouteProvider{}, &paypalRouteStore{}, nil, PayPalSettings{Enabled: true})
	request := httptest.NewRequest(http.MethodGet, "/api/paypal/seller/onboarding/return?state=0123456789abcdef0123456789abcdef&merchantIdInPayPal=seller", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated callback status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
