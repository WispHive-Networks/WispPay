package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/billing"
	"github.com/WispHive-Networks/WispPay/internal/paypal"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

type PayPalClient interface {
	CreateOrder(context.Context, paypal.OrderRequest) (paypal.Order, error)
	CaptureOrder(context.Context, string, string, string, string) (paypal.Capture, error)
	CreateSellerReferral(context.Context, string, string, string, string) (paypal.SellerReferral, error)
	GetSellerStatus(context.Context, string, string, string) (paypal.SellerStatus, error)
	VerifyWebhook(context.Context, string, paypal.WebhookHeaders, json.RawMessage) (bool, error)
}

type paypalStore interface {
	BeginPayPalSellerOnboarding(context.Context, string, string) error
	GetPayPalSeller(context.Context, string) (mysqlstore.PayPalSeller, error)
	CompletePayPalSellerOnboarding(context.Context, string, string, string, bool, bool, bool) error
	RevokePayPalSeller(context.Context, string) error
	GetPublicInvoicePayPal(context.Context, string) (mysqlstore.PayPalInvoice, error)
	StartPayPalPayment(context.Context, string) (mysqlstore.PayPalPayment, error)
	SavePayPalOrder(context.Context, mysqlstore.PayPalPayment) error
	FailPayPalOrder(context.Context, string) error
	ReservePayPalCapture(context.Context, string, string) (mysqlstore.PayPalPayment, error)
	ReleasePayPalCapture(context.Context, string) error
	CompletePayPalCapture(context.Context, mysqlstore.PayPalPayment, string, string, string, string) error
	CompletePayPalWebhookCapture(context.Context, string, string, string, string) error
	ClaimPayPalWebhook(context.Context, string, string) (bool, error)
	FinishPayPalWebhook(context.Context, string) error
	ReleasePayPalWebhook(context.Context, string) error
}

type PayPalSettings struct {
	Enabled           bool
	PartnerMerchantID string
	AttributionID     string
	WebhookID         string
	PublicBaseURL     string
}

type PayPalIntegration struct {
	Client   PayPalClient
	Settings PayPalSettings
}

type paypalHandler struct {
	provider PayPalClient
	store    paypalStore
	identity identityStore
	settings PayPalSettings
}

func registerPayPalRoutes(mux *http.ServeMux, provider PayPalClient, store paypalStore, identity identityStore, settings PayPalSettings) {
	h := paypalHandler{provider: provider, store: store, identity: identity, settings: settings}
	mux.HandleFunc("GET /api/paypal/seller", h.sellerStatus)
	mux.HandleFunc("POST /api/paypal/seller/onboarding", h.startSellerOnboarding)
	mux.HandleFunc("GET /api/paypal/seller/onboarding/return", h.finishSellerOnboarding)
	mux.HandleFunc("POST /api/paypal/webhooks", h.webhook)
	mux.HandleFunc("POST /api/public/invoices/{token}/paypal/orders", h.createOrder)
	mux.HandleFunc("POST /api/public/invoices/{token}/paypal/capture", h.captureOrder)
	mux.HandleFunc("GET /api/public/invoices/{token}/paypal", h.publicInvoicePayment)
}

func (h paypalHandler) available(w http.ResponseWriter) bool {
	if !h.settings.Enabled || h.provider == nil || h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "PayPal payments are not configured"})
		return false
	}
	return true
}

func (h paypalHandler) sellerStatus(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	session, ok := (authHandler{store: h.identity}).currentSession(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	seller, err := h.store.GetPayPalSeller(ctx, session.BusinessID)
	if errors.Is(err, mysqlstore.ErrSellerNotConnected) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "not_connected", "eligible": false})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "PayPal seller status could not be loaded"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": seller.Status, "eligible": seller.Status == "active" && seller.PaymentsReceivable && seller.EmailConfirmed && seller.PaymentPermission})
}

func (h paypalHandler) startSellerOnboarding(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	auth := authHandler{store: h.identity}
	session, ok := auth.currentSession(w, r)
	if !ok || !auth.validCSRF(w, r, session.CSRFHash) {
		return
	}
	state, err := randomPayPalState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "PayPal connection could not be started"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.store.BeginPayPalSellerOnboarding(ctx, session.BusinessID, tokenHash(state)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "PayPal connection could not be started"})
		return
	}
	requestID, err := randomPayPalRequestID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "PayPal connection could not be started"})
		return
	}
	returnURL := h.settings.PublicBaseURL + "/api/paypal/seller/onboarding/return?state=" + url.QueryEscape(state)
	if len(returnURL) > 127 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "PayPal callback URL exceeds the provider limit"})
		return
	}
	referral, err := h.provider.CreateSellerReferral(ctx, h.settings.AttributionID, session.BusinessID, requestID, returnURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "PayPal seller onboarding could not be started"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": referral.ActionURL})
}

func (h paypalHandler) finishSellerOnboarding(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	session, ok := (authHandler{store: h.identity}).currentSession(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	merchantID := r.URL.Query().Get("merchantIdInPayPal")
	if !validPayPalState(state) || !validPayPalMerchantID(merchantID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "PayPal connection return was invalid"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	status, err := h.provider.GetSellerStatus(ctx, h.settings.PartnerMerchantID, merchantID, h.settings.AttributionID)
	if err != nil || status.MerchantID != merchantID || status.TrackingID != session.BusinessID {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "PayPal could not verify this business connection"})
		return
	}
	if err := h.store.CompletePayPalSellerOnboarding(ctx, session.BusinessID, tokenHash(state), merchantID,
		status.PaymentsReceivable, status.EmailConfirmed, status.PaymentPermission); err != nil {
		if errors.Is(err, mysqlstore.ErrSellerStateExpired) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "PayPal connection return expired; start again"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "PayPal connection could not be saved"})
		return
	}
	http.Redirect(w, r, h.settings.PublicBaseURL+"/?paypal=connected", http.StatusSeeOther)
}

func (h paypalHandler) createOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	if !h.available(w) || !h.sameOrigin(w, r) {
		return
	}
	token := r.PathValue("token")
	if !validPublicInvoiceToken(token) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice link is unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	payment, err := h.store.StartPayPalPayment(ctx, token)
	if err != nil {
		h.paymentError(w, err)
		return
	}
	if payment.Status == "created" {
		writeJSON(w, http.StatusOK, map[string]string{"orderId": payment.OrderID, "approvalUrl": payment.ApprovalURL})
		return
	}
	amount, err := billing.ParseMoney(payment.Amount)
	if err != nil || amount <= 0 || !billing.ValidateCurrency(payment.Currency) {
		_ = h.store.FailPayPalOrder(ctx, payment.ID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invoice amount or currency is not payable"})
		return
	}
	order, err := h.provider.CreateOrder(ctx, paypal.OrderRequest{
		InvoiceID: payment.InvoiceID, Reference: payment.Reference, Currency: payment.Currency,
		Amount: billing.FormatMoney(amount), MerchantID: payment.MerchantID,
		ReturnURL: h.settings.PublicBaseURL + "/i/" + token + "?payment=approved",
		CancelURL: h.settings.PublicBaseURL + "/i/" + token + "?payment=cancelled",
		RequestID: payment.CreateRequestID, Attribution: h.settings.AttributionID,
	})
	if err != nil {
		_ = h.store.FailPayPalOrder(ctx, payment.ID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "PayPal checkout could not be created"})
		return
	}
	payment.OrderID = order.ID
	payment.ApprovalURL = order.ApprovalURL
	if err := h.store.SavePayPalOrder(ctx, payment); err != nil {
		_ = h.store.FailPayPalOrder(ctx, payment.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "PayPal checkout could not be saved"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"orderId": order.ID, "approvalUrl": order.ApprovalURL})
}

func (h paypalHandler) publicInvoicePayment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	if !validPublicInvoiceToken(r.PathValue("token")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice link is unavailable"})
		return
	}
	if !h.settings.Enabled || h.provider == nil || h.store == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"available": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	invoice, err := h.store.GetPublicInvoicePayPal(ctx, r.PathValue("token"))
	if errors.Is(err, mysqlstore.ErrSellerNotConnected) {
		writeJSON(w, http.StatusOK, map[string]bool{"available": false})
		return
	}
	if errors.Is(err, mysqlstore.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice link is unavailable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "payment availability could not be checked"})
		return
	}
	amount, err := billing.ParseMoney(invoice.Amount)
	available := err == nil && amount > 0 && invoice.Status == "open" && billing.ValidateCurrency(invoice.Currency)
	writeJSON(w, http.StatusOK, map[string]bool{"available": available})
}

func (h paypalHandler) captureOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	if !h.available(w) || !h.sameOrigin(w, r) {
		return
	}
	token := r.PathValue("token")
	if !validPublicInvoiceToken(token) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice link is unavailable"})
		return
	}
	var input struct {
		OrderID string `json:"orderId"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	if !validPayPalOrderID(input.OrderID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid PayPal order"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	payment, err := h.store.ReservePayPalCapture(ctx, token, input.OrderID)
	if err != nil {
		h.paymentError(w, err)
		return
	}
	if payment.Status == "completed" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
		return
	}
	capture, err := h.provider.CaptureOrder(ctx, payment.OrderID, payment.CaptureRequestID, h.settings.AttributionID, payment.MerchantID)
	if err != nil {
		_ = h.store.ReleasePayPalCapture(ctx, payment.ID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "PayPal payment could not be confirmed"})
		return
	}
	if capture.OrderID != payment.OrderID || capture.OrderStatus != "COMPLETED" || capture.CaptureStatus != "COMPLETED" {
		_ = h.store.ReleasePayPalCapture(ctx, payment.ID)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "PayPal payment is not complete"})
		return
	}
	if err := h.store.CompletePayPalCapture(ctx, payment, capture.OrderID, capture.CaptureID, capture.Currency, capture.Amount); err != nil {
		_ = h.store.ReleasePayPalCapture(ctx, payment.ID)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "PayPal payment did not match this invoice"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (h paypalHandler) webhook(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	if h.settings.WebhookID == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "PayPal webhook verification is not configured"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	eventBytes, err := io.ReadAll(r.Body)
	if err != nil || !json.Valid(eventBytes) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook event"})
		return
	}
	headers := paypal.WebhookHeaders{
		AuthAlgorithm: r.Header.Get("PAYPAL-AUTH-ALGO"), CertificateURL: r.Header.Get("PAYPAL-CERT-URL"),
		TransmissionID: r.Header.Get("PAYPAL-TRANSMISSION-ID"), TransmissionSig: r.Header.Get("PAYPAL-TRANSMISSION-SIG"),
		TransmissionTime: r.Header.Get("PAYPAL-TRANSMISSION-TIME"),
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	verified, err := h.provider.VerifyWebhook(ctx, h.settings.WebhookID, headers, eventBytes)
	if err != nil || !verified {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "webhook signature is invalid"})
		return
	}
	var event struct {
		ID       string `json:"id"`
		Type     string `json:"event_type"`
		Resource struct {
			ID         string `json:"id"`
			Status     string `json:"status"`
			MerchantID string `json:"merchant_id"`
			Amount     struct {
				Currency string `json:"currency_code"`
				Value    string `json:"value"`
			} `json:"amount"`
			SupplementaryData struct {
				RelatedIDs struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(eventBytes, &event); err != nil || event.ID == "" || len(event.ID) > 128 || event.Type == "" || len(event.Type) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook event"})
		return
	}
	claimed, err := h.store.ClaimPayPalWebhook(ctx, event.ID, event.Type)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "webhook could not be recorded"})
		return
	}
	if !claimed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	processErr := h.processWebhookEvent(ctx, event.Type, event.Resource.ID, event.Resource.Status,
		event.Resource.MerchantID, event.Resource.SupplementaryData.RelatedIDs.OrderID,
		event.Resource.Amount.Currency, event.Resource.Amount.Value)
	if processErr != nil {
		_ = h.store.ReleasePayPalWebhook(ctx, event.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "webhook event could not be processed"})
		return
	}
	if err := h.store.FinishPayPalWebhook(ctx, event.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "webhook event could not be finalized"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h paypalHandler) processWebhookEvent(ctx context.Context, eventType, resourceID, status, merchantID, orderID, currency, amount string) error {
	switch eventType {
	case "PAYMENT.CAPTURE.COMPLETED":
		if status != "COMPLETED" || resourceID == "" || orderID == "" || !billing.ValidateCurrency(currency) {
			return errors.New("invalid completed capture webhook")
		}
		err := h.store.CompletePayPalWebhookCapture(ctx, orderID, resourceID, currency, amount)
		if errors.Is(err, mysqlstore.ErrNotFound) {
			return nil
		}
		return err
	case "MERCHANT.PARTNER-CONSENT.REVOKED":
		if !validPayPalMerchantID(merchantID) {
			return errors.New("invalid seller revocation webhook")
		}
		return h.store.RevokePayPalSeller(ctx, merchantID)
	default:
		return nil
	}
}

func (h paypalHandler) paymentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mysqlstore.ErrNotFound), errors.Is(err, mysqlstore.ErrSellerNotConnected):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice or payment method is unavailable"})
	case errors.Is(err, mysqlstore.ErrInvoiceNotPayable):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "invoice is not payable"})
	case errors.Is(err, mysqlstore.ErrPaymentInProgress):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "payment is already being processed"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "payment could not be started"})
	}
}

func (h paypalHandler) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	configured, configuredErr := url.Parse(h.settings.PublicBaseURL)
	if err != nil || configuredErr != nil || origin.Scheme != configured.Scheme || !strings.EqualFold(origin.Host, configured.Host) || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request origin is not allowed"})
		return false
	}
	return true
}

func randomPayPalState() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate PayPal state")
	}
	return hex.EncodeToString(value[:]), nil
}

func randomPayPalRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate PayPal request ID")
	}
	return hex.EncodeToString(value[:]), nil
}

func validPayPalState(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPublicInvoiceToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPayPalMerchantID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validPayPalOrderID(value string) bool {
	if len(value) < 1 || len(value) > 36 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
