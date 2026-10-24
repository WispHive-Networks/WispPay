package paypal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 1 << 20

type Client struct {
	clientID     string
	clientSecret string
	baseURL      string
	httpClient   *http.Client
	tokenMu      sync.Mutex
	token        string
	tokenExpiry  time.Time
}

type OrderRequest struct {
	InvoiceID   string
	Reference   string
	Currency    string
	Amount      string
	MerchantID  string
	ReturnURL   string
	CancelURL   string
	RequestID   string
	Attribution string
}

type Order struct {
	ID          string
	Status      string
	ApprovalURL string
}

type Capture struct {
	OrderID       string
	OrderStatus   string
	CaptureID     string
	CaptureStatus string
	Currency      string
	Amount        string
}

type SellerReferral struct {
	ActionURL string
}

type SellerStatus struct {
	TrackingID         string
	MerchantID         string
	PaymentsReceivable bool
	EmailConfirmed     bool
	PaymentPermission  bool
}

type WebhookHeaders struct {
	AuthAlgorithm    string
	CertificateURL   string
	TransmissionID   string
	TransmissionSig  string
	TransmissionTime string
}

type webhookVerification struct {
	VerificationStatus string `json:"verification_status"`
}

func NewClient(clientID, clientSecret, baseURL string, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return nil, errors.New("PayPal client credentials are required")
	}
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, errors.New("invalid PayPal API base URL")
	}
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = nil
		httpClient = &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{clientID: clientID, clientSecret: clientSecret, baseURL: strings.TrimRight(parsed.String(), "/"), httpClient: httpClient}, nil
}

func (c *Client) CreateOrder(ctx context.Context, input OrderRequest) (Order, error) {
	if input.InvoiceID == "" || input.Reference == "" || input.Currency == "" || input.Amount == "" || input.MerchantID == "" || input.RequestID == "" || input.Attribution == "" {
		return Order{}, errors.New("incomplete PayPal order request")
	}
	payload := struct {
		Intent        string `json:"intent"`
		PurchaseUnits []struct {
			ReferenceID string `json:"reference_id"`
			InvoiceID   string `json:"invoice_id"`
			CustomID    string `json:"custom_id"`
			Payee       struct {
				MerchantID string `json:"merchant_id"`
			} `json:"payee"`
			Amount struct {
				Currency string `json:"currency_code"`
				Value    string `json:"value"`
			} `json:"amount"`
		} `json:"purchase_units"`
		PaymentSource struct {
			PayPal struct {
				ExperienceContext struct {
					ReturnURL          string `json:"return_url"`
					CancelURL          string `json:"cancel_url"`
					UserAction         string `json:"user_action"`
					ShippingPreference string `json:"shipping_preference"`
				} `json:"experience_context"`
			} `json:"paypal"`
		} `json:"payment_source"`
	}{Intent: "CAPTURE"}
	payload.PurchaseUnits = make([]struct {
		ReferenceID string `json:"reference_id"`
		InvoiceID   string `json:"invoice_id"`
		CustomID    string `json:"custom_id"`
		Payee       struct {
			MerchantID string `json:"merchant_id"`
		} `json:"payee"`
		Amount struct {
			Currency string `json:"currency_code"`
			Value    string `json:"value"`
		} `json:"amount"`
	}, 1)
	unit := &payload.PurchaseUnits[0]
	unit.ReferenceID = input.InvoiceID
	unit.InvoiceID = input.Reference
	unit.CustomID = input.InvoiceID
	unit.Payee.MerchantID = input.MerchantID
	unit.Amount.Currency = input.Currency
	unit.Amount.Value = input.Amount
	payload.PaymentSource.PayPal.ExperienceContext.ReturnURL = input.ReturnURL
	payload.PaymentSource.PayPal.ExperienceContext.CancelURL = input.CancelURL
	payload.PaymentSource.PayPal.ExperienceContext.UserAction = "PAY_NOW"
	payload.PaymentSource.PayPal.ExperienceContext.ShippingPreference = "NO_SHIPPING"
	body, err := json.Marshal(payload)
	if err != nil {
		return Order{}, fmt.Errorf("encode PayPal order: %w", err)
	}
	var response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Links  []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := c.apiJSON(ctx, http.MethodPost, "/v2/checkout/orders", input.RequestID, input.Attribution, input.MerchantID, body, &response); err != nil {
		return Order{}, err
	}
	order := Order{ID: response.ID, Status: response.Status}
	for _, link := range response.Links {
		if link.Rel == "payer-action" || link.Rel == "approve" {
			if !safePayPalRedirect(link.Href) {
				return Order{}, errors.New("PayPal returned an unsafe approval URL")
			}
			order.ApprovalURL = link.Href
			break
		}
	}
	if order.ID == "" || order.ApprovalURL == "" {
		return Order{}, errors.New("PayPal returned an incomplete approval order")
	}
	return order, nil
}

func (c *Client) CaptureOrder(ctx context.Context, orderID, requestID, attribution, merchantID string) (Capture, error) {
	if orderID == "" || requestID == "" || attribution == "" || merchantID == "" || strings.ContainsAny(orderID, "/?#") {
		return Capture{}, errors.New("invalid PayPal capture request")
	}
	var response struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		PurchaseUnits []struct {
			Payments struct {
				Captures []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Amount struct {
						Currency string `json:"currency_code"`
						Value    string `json:"value"`
					} `json:"amount"`
				} `json:"captures"`
			} `json:"payments"`
		} `json:"purchase_units"`
	}
	if err := c.apiJSON(ctx, http.MethodPost, "/v2/checkout/orders/"+url.PathEscape(orderID)+"/capture", requestID, attribution, merchantID, []byte("{}"), &response); err != nil {
		return Capture{}, err
	}
	if len(response.PurchaseUnits) != 1 || len(response.PurchaseUnits[0].Payments.Captures) != 1 {
		return Capture{}, errors.New("PayPal returned an incomplete capture")
	}
	item := response.PurchaseUnits[0].Payments.Captures[0]
	return Capture{OrderID: response.ID, OrderStatus: response.Status, CaptureID: item.ID,
		CaptureStatus: item.Status, Currency: item.Amount.Currency, Amount: item.Amount.Value}, nil
}

func (c *Client) VerifyWebhook(ctx context.Context, webhookID string, headers WebhookHeaders, event json.RawMessage) (bool, error) {
	if webhookID == "" || headers.AuthAlgorithm == "" || headers.CertificateURL == "" || headers.TransmissionID == "" || headers.TransmissionSig == "" || headers.TransmissionTime == "" || !json.Valid(event) || len(event) > maxResponseBytes {
		return false, errors.New("incomplete PayPal webhook verification request")
	}
	fields := []string{headers.AuthAlgorithm, headers.CertificateURL, headers.TransmissionID, headers.TransmissionSig, headers.TransmissionTime, webhookID}
	encoded := make([][]byte, len(fields))
	for index, field := range fields {
		value, err := json.Marshal(field)
		if err != nil {
			return false, fmt.Errorf("encode PayPal webhook verification: %w", err)
		}
		encoded[index] = value
	}
	body := []byte(`{"auth_algo":`)
	body = append(body, encoded[0]...)
	body = append(body, `,"cert_url":`...)
	body = append(body, encoded[1]...)
	body = append(body, `,"transmission_id":`...)
	body = append(body, encoded[2]...)
	body = append(body, `,"transmission_sig":`...)
	body = append(body, encoded[3]...)
	body = append(body, `,"transmission_time":`...)
	body = append(body, encoded[4]...)
	body = append(body, `,"webhook_id":`...)
	body = append(body, encoded[5]...)
	body = append(body, `,"webhook_event":`...)
	body = append(body, event...)
	body = append(body, '}')
	var result webhookVerification
	if err := c.apiJSON(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", "", "", "", body, &result); err != nil {
		return false, err
	}
	return result.VerificationStatus == "SUCCESS", nil
}

func (c *Client) CreateSellerReferral(ctx context.Context, attributionID, trackingID, requestID, returnURL string) (SellerReferral, error) {
	if attributionID == "" || trackingID == "" || requestID == "" || returnURL == "" {
		return SellerReferral{}, errors.New("incomplete PayPal seller referral request")
	}
	payload := struct {
		TrackingID string `json:"tracking_id"`
		Operations []struct {
			Operation  string `json:"operation"`
			Preference struct {
				REST struct {
					Method     string `json:"integration_method"`
					Channel    string `json:"integration_channel"`
					Type       string `json:"integration_type"`
					ThirdParty struct {
						Features   []string `json:"features"`
						SignupMode string   `json:"signup_mode"`
					} `json:"third_party_details"`
				} `json:"rest_api_integration"`
			} `json:"api_integration_preference"`
		} `json:"operations"`
		Products []string `json:"products"`
		Consents []struct {
			Type    string `json:"type"`
			Granted bool   `json:"granted"`
		} `json:"legal_consents"`
		PartnerConfig struct {
			ReturnURL string `json:"return_url"`
		} `json:"partner_config_override"`
	}{TrackingID: trackingID, Products: []string{"EXPRESS_CHECKOUT"}}
	payload.Operations = make([]struct {
		Operation  string `json:"operation"`
		Preference struct {
			REST struct {
				Method     string `json:"integration_method"`
				Channel    string `json:"integration_channel"`
				Type       string `json:"integration_type"`
				ThirdParty struct {
					Features   []string `json:"features"`
					SignupMode string   `json:"signup_mode"`
				} `json:"third_party_details"`
			} `json:"rest_api_integration"`
		} `json:"api_integration_preference"`
	}, 1)
	operation := &payload.Operations[0]
	operation.Operation = "API_INTEGRATION"
	operation.Preference.REST.Method = "PAYPAL"
	operation.Preference.REST.Channel = "DEFERRED_ONBOARDING"
	operation.Preference.REST.Type = "THIRD_PARTY"
	operation.Preference.REST.ThirdParty.Features = []string{"PAYMENT", "REFUND"}
	operation.Preference.REST.ThirdParty.SignupMode = "VERIFY_WITH_PAYPAL"
	payload.Consents = make([]struct {
		Type    string `json:"type"`
		Granted bool   `json:"granted"`
	}, 1)
	payload.Consents[0].Type = "SHARE_DATA_CONSENT"
	payload.Consents[0].Granted = true
	payload.PartnerConfig.ReturnURL = returnURL
	body, err := json.Marshal(payload)
	if err != nil {
		return SellerReferral{}, fmt.Errorf("encode PayPal referral: %w", err)
	}
	var response struct {
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := c.apiJSON(ctx, http.MethodPost, "/v2/customer/partner-referrals", requestID, attributionID, "", body, &response); err != nil {
		return SellerReferral{}, err
	}
	for _, link := range response.Links {
		if link.Rel == "action_url" {
			if !safePayPalRedirect(link.Href) {
				return SellerReferral{}, errors.New("PayPal returned an unsafe seller action URL")
			}
			return SellerReferral{ActionURL: link.Href}, nil
		}
	}
	return SellerReferral{}, errors.New("PayPal returned no seller action URL")
}

func safePayPalRedirect(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	switch parsed.Hostname() {
	case "www.sandbox.paypal.com", "www.paypal.com":
		return true
	default:
		return false
	}
}

func (c *Client) GetSellerStatus(ctx context.Context, partnerMerchantID, sellerMerchantID, attributionID string) (SellerStatus, error) {
	if partnerMerchantID == "" || sellerMerchantID == "" || attributionID == "" || strings.ContainsAny(partnerMerchantID+sellerMerchantID, "/?#") {
		return SellerStatus{}, errors.New("invalid PayPal seller status request")
	}
	path := "/v1/customer/partners/" + url.PathEscape(partnerMerchantID) + "/merchant-integrations/" + url.PathEscape(sellerMerchantID)
	var response struct {
		TrackingID         string `json:"tracking_id"`
		MerchantID         string `json:"merchant_id"`
		PaymentsReceivable bool   `json:"payments_receivable"`
		EmailConfirmed     bool   `json:"primary_email_confirmed"`
		Integrations       []struct {
			Type       string `json:"integration_type"`
			ThirdParty []struct {
				Scopes []string `json:"scopes"`
			} `json:"oauth_third_party"`
		} `json:"oauth_integrations"`
	}
	if err := c.apiJSON(ctx, http.MethodGet, path, "", attributionID, "", nil, &response); err != nil {
		return SellerStatus{}, err
	}
	permission := false
	for _, integration := range response.Integrations {
		if integration.Type != "OAUTH_THIRD_PARTY" {
			continue
		}
		for _, thirdParty := range integration.ThirdParty {
			for _, scope := range thirdParty.Scopes {
				if strings.HasSuffix(scope, "/services/payments/payment/authcapture") {
					permission = true
				}
			}
		}
	}
	return SellerStatus{TrackingID: response.TrackingID, MerchantID: response.MerchantID, PaymentsReceivable: response.PaymentsReceivable,
		EmailConfirmed: response.EmailConfirmed, PaymentPermission: permission}, nil
}

func (c *Client) apiJSON(ctx context.Context, method, path, requestID, attribution, sellerMerchantID string, body []byte, result any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build PayPal request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Prefer", "return=representation")
	if sellerMerchantID != "" {
		assertion, err := c.authAssertion(sellerMerchantID)
		if err != nil {
			return fmt.Errorf("build PayPal seller assertion: %w", err)
		}
		request.Header.Set("PayPal-Auth-Assertion", assertion)
	}
	if requestID != "" {
		request.Header.Set("PayPal-Request-Id", requestID)
	}
	if attribution != "" {
		request.Header.Set("PayPal-Partner-Attribution-Id", attribution)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("PayPal API request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read PayPal API response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return errors.New("PayPal API response exceeded size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("PayPal API returned HTTP %d", response.StatusCode)
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return fmt.Errorf("decode PayPal API response: %w", err)
	}
	return nil
}

func (c *Client) authAssertion(merchantID string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(struct {
		Issuer     string `json:"iss"`
		MerchantID string `json:"payer_id"`
	}{Issuer: c.clientID, MerchantID: merchantID})
	if err != nil {
		return "", err
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".", nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" && time.Until(c.tokenExpiry) > time.Minute {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build PayPal token request: %w", err)
	}
	request.SetBasicAuth(c.clientID, c.clientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("PayPal OAuth request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read PayPal OAuth response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return "", errors.New("PayPal OAuth response exceeded size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("PayPal OAuth returned HTTP %d", response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(responseBody, &token); err != nil || token.AccessToken == "" || token.ExpiresIn <= 0 {
		return "", errors.New("PayPal OAuth response was invalid")
	}
	c.token = token.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return c.token, nil
}
