package paypal

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCreateAndCaptureOrderUseConnectedSellerAndStableRequestIDs(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth2/token":
			tokenCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || string(body) != "grant_type=client_credentials" {
				t.Errorf("OAuth request = method %q, form %q", r.Method, body)
			}
			username, password, ok := r.BasicAuth()
			if !ok || username != "client-id" || password != "client-secret" {
				t.Errorf("OAuth basic auth = (%q, %q, %v)", username, password, ok)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"access-token","expires_in":3600}`)
		case "/v2/checkout/orders":
			if r.Header.Get("PayPal-Request-Id") != "create-request-1" || r.Header.Get("PayPal-Partner-Attribution-Id") != "BN-CODE" {
				t.Errorf("order headers missing idempotency or attribution: %v", r.Header)
			}
			assertSellerAuthorization(t, r, "client-id", "SELLER-MERCHANT")
			var body struct {
				Intent        string `json:"intent"`
				PurchaseUnits []struct {
					InvoiceID string `json:"invoice_id"`
					CustomID  string `json:"custom_id"`
					Payee     struct {
						MerchantID string `json:"merchant_id"`
					} `json:"payee"`
					Amount struct {
						Currency string `json:"currency_code"`
						Value    string `json:"value"`
					} `json:"amount"`
				} `json:"purchase_units"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode order body: %v", err)
			}
			if body.Intent != "CAPTURE" || len(body.PurchaseUnits) != 1 || body.PurchaseUnits[0].Payee.MerchantID != "SELLER-MERCHANT" || body.PurchaseUnits[0].Amount.Currency != "USD" || body.PurchaseUnits[0].Amount.Value != "25.50" || body.PurchaseUnits[0].CustomID != "invoice-1" {
				t.Errorf("unexpected order body: %+v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"ORDER-1","status":"PAYER_ACTION_REQUIRED","links":[{"rel":"payer-action","href":"https://www.sandbox.paypal.com/checkout?token=ORDER-1"}]}`)
		case "/v2/checkout/orders/ORDER-1/capture":
			if r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("PayPal-Request-Id") != "capture-request-1" {
				t.Errorf("capture headers are incorrect: %v", r.Header)
			}
			assertSellerAuthorization(t, r, "client-id", "SELLER-MERCHANT")
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"ORDER-1","status":"COMPLETED","purchase_units":[{"payments":{"captures":[{"id":"CAPTURE-1","status":"COMPLETED","amount":{"currency_code":"USD","value":"25.50"}}]}}]}`)
		default:
			t.Errorf("unexpected PayPal path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient("client-id", "client-secret", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	order, err := client.CreateOrder(t.Context(), OrderRequest{
		InvoiceID: "invoice-1", Reference: "WP-1234567890", Currency: "USD", Amount: "25.50",
		MerchantID: "SELLER-MERCHANT", ReturnURL: "https://wisppay.example/i/token?payment=approved",
		CancelURL: "https://wisppay.example/i/token?payment=cancelled", RequestID: "create-request-1", Attribution: "BN-CODE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.ID != "ORDER-1" || order.ApprovalURL != "https://www.sandbox.paypal.com/checkout?token=ORDER-1" {
		t.Fatalf("created order = %+v", order)
	}
	capture, err := client.CaptureOrder(t.Context(), order.ID, "capture-request-1", "BN-CODE", "SELLER-MERCHANT")
	if err != nil {
		t.Fatal(err)
	}
	if capture.OrderStatus != "COMPLETED" || capture.CaptureID != "CAPTURE-1" || capture.CaptureStatus != "COMPLETED" || capture.Currency != "USD" || capture.Amount != "25.50" {
		t.Fatalf("capture = %+v", capture)
	}
	if tokenCalls.Load() != 1 {
		t.Fatalf("OAuth called %d times, want cached token reused", tokenCalls.Load())
	}
}

func assertSellerAuthorization(t *testing.T, r *http.Request, clientID, merchantID string) {
	t.Helper()
	parts := strings.Split(r.Header.Get("PayPal-Auth-Assertion"), ".")
	if len(parts) != 3 || parts[2] != "" {
		t.Errorf("PayPal seller assertion is not an unsigned JWT: %q", r.Header.Get("PayPal-Auth-Assertion"))
		return
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || string(header) != `{"alg":"none"}` {
		t.Errorf("PayPal seller assertion header = %q, err %v", header, err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("decode PayPal seller assertion payload: %v", err)
		return
	}
	var claims struct {
		Issuer     string `json:"iss"`
		MerchantID string `json:"payer_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Issuer != clientID || claims.MerchantID != merchantID {
		t.Errorf("PayPal seller assertion claims = %+v, err %v", claims, err)
	}
}

func TestVerifyWebhookPreservesEventBytes(t *testing.T) {
	event := json.RawMessage("{ \"id\": \"EVENT-1\",  \"event_type\": \"PAYMENT.CAPTURE.COMPLETED\" }")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"access-token","expires_in":3600}`)
			return
		}
		if r.URL.Path != "/v1/notifications/verify-webhook-signature" {
			t.Errorf("unexpected PayPal path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body struct {
			Event json.RawMessage `json:"webhook_event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode verification request: %v", err)
		}
		if string(body.Event) != string(event) {
			t.Errorf("webhook event changed: got %q, want %q", body.Event, event)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"verification_status":"SUCCESS"}`)
	}))
	defer server.Close()
	client, err := NewClient("client-id", "client-secret", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	verified, err := client.VerifyWebhook(t.Context(), "WEBHOOK-ID", WebhookHeaders{
		AuthAlgorithm: "SHA256withRSA", CertificateURL: "https://api-m.sandbox.paypal.com/certs/cert-1",
		TransmissionID: "transmission-1", TransmissionSig: "signature", TransmissionTime: "2026-10-24T12:00:00Z",
	}, event)
	if err != nil || !verified {
		t.Fatalf("VerifyWebhook = (%v, %v)", verified, err)
	}
}

func TestSellerReferralAndEligibilityVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"access-token","expires_in":3600}`)
			return
		}
		switch r.URL.Path {
		case "/v2/customer/partner-referrals":
			if r.Method != http.MethodPost || r.Header.Get("PayPal-Partner-Attribution-Id") != "BN-CODE" || r.Header.Get("PayPal-Request-Id") != "request-1" {
				t.Errorf("referral request headers/method are incorrect: %s %v", r.Method, r.Header)
			}
			var request struct {
				TrackingID string   `json:"tracking_id"`
				Products   []string `json:"products"`
				Consents   []struct {
					Type    string `json:"type"`
					Granted bool   `json:"granted"`
				} `json:"legal_consents"`
				Operations []struct {
					Operation  string `json:"operation"`
					Preference struct {
						REST struct {
							Channel    string `json:"integration_channel"`
							Type       string `json:"integration_type"`
							ThirdParty struct {
								Features   []string `json:"features"`
								SignupMode string   `json:"signup_mode"`
							} `json:"third_party_details"`
						} `json:"rest_api_integration"`
					} `json:"api_integration_preference"`
				} `json:"operations"`
				PartnerConfig struct {
					ReturnURL string `json:"return_url"`
				} `json:"partner_config_override"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode referral request: %v", err)
			}
			if request.TrackingID != "business-1" || len(request.Products) != 1 || request.Products[0] != "EXPRESS_CHECKOUT" || len(request.Consents) != 1 || !request.Consents[0].Granted || len(request.Operations) != 1 || request.Operations[0].Operation != "API_INTEGRATION" || request.Operations[0].Preference.REST.Channel != "DEFERRED_ONBOARDING" || request.Operations[0].Preference.REST.Type != "THIRD_PARTY" || request.Operations[0].Preference.REST.ThirdParty.SignupMode != "VERIFY_WITH_PAYPAL" || len(request.Operations[0].Preference.REST.ThirdParty.Features) != 2 || request.PartnerConfig.ReturnURL != "https://wisppay.example/api/paypal/onboarding/return" {
				t.Errorf("unexpected referral payload: %+v", request)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"links":[{"rel":"action_url","href":"https://www.sandbox.paypal.com/partnerexp/appEntry?referralToken=abc"}]}`)
		case "/v1/customer/partners/PARTNER-MERCHANT/merchant-integrations/SELLER-MERCHANT":
			if r.Method != http.MethodGet || r.Header.Get("PayPal-Partner-Attribution-Id") != "BN-CODE" {
				t.Errorf("seller lookup headers/method are incorrect: %s %v", r.Method, r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"tracking_id":"business-1","merchant_id":"SELLER-MERCHANT","payments_receivable":true,"primary_email_confirmed":true,"oauth_integrations":[{"integration_type":"OAUTH_THIRD_PARTY","oauth_third_party":[{"scopes":["https://uri.paypal.com/services/payments/payment/authcapture"]}]}]}`)
		default:
			t.Errorf("unexpected PayPal path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient("client-id", "client-secret", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	referral, err := client.CreateSellerReferral(t.Context(), "BN-CODE", "business-1", "request-1", "https://wisppay.example/api/paypal/onboarding/return")
	if err != nil {
		t.Fatal(err)
	}
	if referral.ActionURL != "https://www.sandbox.paypal.com/partnerexp/appEntry?referralToken=abc" {
		t.Fatalf("referral action URL = %q", referral.ActionURL)
	}
	status, err := client.GetSellerStatus(t.Context(), "PARTNER-MERCHANT", "SELLER-MERCHANT", "BN-CODE")
	if err != nil {
		t.Fatal(err)
	}
	if status.TrackingID != "business-1" || status.MerchantID != "SELLER-MERCHANT" || !status.PaymentsReceivable || !status.EmailConfirmed || !status.PaymentPermission {
		t.Fatalf("seller status = %+v", status)
	}
}

func TestProviderRejectsOAuthErrorsAndUntrustedApprovalURLs(t *testing.T) {
	for _, test := range []struct {
		name        string
		oauthStatus int
		approvalURL string
	}{
		{name: "OAuth failure", oauthStatus: http.StatusUnauthorized},
		{name: "untrusted approval URL", oauthStatus: http.StatusOK, approvalURL: "https://attacker.example/checkout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/oauth2/token" {
					if test.oauthStatus != http.StatusOK {
						w.WriteHeader(test.oauthStatus)
						io.WriteString(w, "private provider details")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"access_token":"access-token","expires_in":3600}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"id":"ORDER-1","status":"PAYER_ACTION_REQUIRED","links":[{"rel":"payer-action","href":"`+test.approvalURL+`"}]}`)
			}))
			defer server.Close()
			client, err := NewClient("client-id", "client-secret", server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateOrder(t.Context(), OrderRequest{
				InvoiceID: "invoice-id", Reference: "WP-1", Currency: "USD", Amount: "10.00",
				MerchantID: "seller-id", RequestID: "request-id", Attribution: "BN-CODE",
			})
			if err == nil {
				t.Fatal("expected PayPal request to fail")
			}
			if strings.Contains(err.Error(), "private provider details") {
				t.Fatal("PayPal error body leaked to caller")
			}
		})
	}
}

func TestCaptureRejectsInvalidOrderIDWithoutRequest(t *testing.T) {
	client, err := NewClient("client-id", "client-secret", "https://api-m.sandbox.paypal.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CaptureOrder(t.Context(), "../other-order", "request-id", "BN-CODE", "SELLER-MERCHANT"); err == nil {
		t.Fatal("expected invalid order ID to be rejected")
	}
}

func TestNewClientRejectsUnsafeBaseURLs(t *testing.T) {
	for _, baseURL := range []string{"", "ftp://api-m.paypal.com", "https://user@api-m.paypal.com", "https://api-m.paypal.com/path", "https://api-m.paypal.com?redirect=1"} {
		if _, err := NewClient("client-id", "client-secret", baseURL, nil); err == nil {
			t.Errorf("NewClient accepted unsafe URL %q", baseURL)
		}
	}
}
