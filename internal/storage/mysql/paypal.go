package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/billing"
)

var (
	ErrSellerNotConnected = errors.New("PayPal seller is not eligible")
	ErrSellerStateExpired = errors.New("PayPal onboarding state is invalid or expired")
	ErrInvoiceNotPayable  = errors.New("invoice is not payable")
	ErrPaymentInProgress  = errors.New("PayPal payment operation is already in progress")
)

type PayPalSeller struct {
	MerchantID         string
	Status             string
	PaymentsReceivable bool
	EmailConfirmed     bool
	PaymentPermission  bool
}

type PayPalPayment struct {
	ID               string
	BusinessID       string
	InvoiceID        string
	Reference        string
	OrderID          string
	ApprovalURL      string
	CreateRequestID  string
	CaptureRequestID string
	CaptureID        string
	Currency         string
	Amount           string
	Status           string
	MerchantID       string
}

type PayPalInvoice struct {
	BusinessID string
	InvoiceID  string
	Reference  string
	Currency   string
	Amount     string
	Status     string
	MerchantID string
}

func (db *DB) BeginPayPalSellerOnboarding(ctx context.Context, businessID, stateHash string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO paypal_seller_accounts (business_id, tracking_id, onboarding_state_hash, status)
		VALUES (?, ?, ?, 'pending') ON DUPLICATE KEY UPDATE
		onboarding_state_hash = IF(status = 'active', onboarding_state_hash, VALUES(onboarding_state_hash)),
		status = IF(status = 'active', status, 'pending')`, businessID, businessID, stateHash)
	if err != nil {
		return fmt.Errorf("begin PayPal seller onboarding: %w", err)
	}
	return nil
}

func (db *DB) GetPayPalSeller(ctx context.Context, businessID string) (PayPalSeller, error) {
	var seller PayPalSeller
	err := db.QueryRowContext(ctx, `SELECT COALESCE(merchant_id, ''), status, payments_receivable,
		primary_email_confirmed, payment_permission FROM paypal_seller_accounts WHERE business_id = ?`, businessID).
		Scan(&seller.MerchantID, &seller.Status, &seller.PaymentsReceivable, &seller.EmailConfirmed, &seller.PaymentPermission)
	if errors.Is(err, sql.ErrNoRows) {
		return PayPalSeller{}, ErrSellerNotConnected
	}
	if err != nil {
		return PayPalSeller{}, fmt.Errorf("get PayPal seller: %w", err)
	}
	return seller, nil
}

func (db *DB) CompletePayPalSellerOnboarding(ctx context.Context, businessID, stateHash, merchantID string, paymentsReceivable, emailConfirmed, paymentPermission bool) error {
	result, err := db.ExecContext(ctx, `UPDATE paypal_seller_accounts SET merchant_id = ?,
		payments_receivable = ?, primary_email_confirmed = ?, payment_permission = ?,
		status = IF(? AND ? AND ?, 'active', 'pending'),
		connected_at = IF(? AND ? AND ?, UTC_TIMESTAMP(), NULL), onboarding_state_hash = NULL
		WHERE business_id = ? AND tracking_id = ? AND onboarding_state_hash = ?`,
		merchantID, paymentsReceivable, emailConfirmed, paymentPermission,
		paymentsReceivable, emailConfirmed, paymentPermission, paymentsReceivable, emailConfirmed, paymentPermission,
		businessID, businessID, stateHash)
	if err != nil {
		return fmt.Errorf("complete PayPal seller onboarding: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check PayPal onboarding state: %w", err)
	}
	if rows != 1 {
		return ErrSellerStateExpired
	}
	return nil
}

func (db *DB) RevokePayPalSeller(ctx context.Context, merchantID string) error {
	_, err := db.ExecContext(ctx, `UPDATE paypal_seller_accounts SET status = 'revoked',
		payments_receivable = FALSE, payment_permission = FALSE WHERE merchant_id = ?`, merchantID)
	if err != nil {
		return fmt.Errorf("revoke PayPal seller: %w", err)
	}
	return nil
}

func (db *DB) GetPublicInvoicePayPal(ctx context.Context, token string) (PayPalInvoice, error) {
	var invoice PayPalInvoice
	err := db.QueryRowContext(ctx, `SELECT i.business_id, i.id, i.invoice_reference, i.currency,
		CAST(i.total AS CHAR), i.status, COALESCE(s.merchant_id, '')
		FROM public_invoice_tokens t JOIN invoices i ON i.business_id = t.business_id AND i.id = t.invoice_id
		LEFT JOIN paypal_seller_accounts s ON s.business_id = i.business_id AND s.status = 'active'
			AND s.payments_receivable = TRUE AND s.primary_email_confirmed = TRUE AND s.payment_permission = TRUE
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > UTC_TIMESTAMP()`, hashToken(token)).
		Scan(&invoice.BusinessID, &invoice.InvoiceID, &invoice.Reference, &invoice.Currency, &invoice.Amount, &invoice.Status, &invoice.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return PayPalInvoice{}, ErrNotFound
	}
	if err != nil {
		return PayPalInvoice{}, fmt.Errorf("get public invoice payment context: %w", err)
	}
	if invoice.MerchantID == "" {
		return PayPalInvoice{}, ErrSellerNotConnected
	}
	return invoice, nil
}

func (db *DB) StartPayPalPayment(ctx context.Context, token string) (PayPalPayment, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return PayPalPayment{}, fmt.Errorf("begin PayPal payment: %w", err)
	}
	defer tx.Rollback()
	var payment PayPalPayment
	var status string
	err = tx.QueryRowContext(ctx, `SELECT i.business_id, i.id, i.invoice_reference, i.currency,
		CAST(i.total AS CHAR), i.status, s.merchant_id FROM public_invoice_tokens t
		JOIN invoices i ON i.business_id = t.business_id AND i.id = t.invoice_id
		JOIN paypal_seller_accounts s ON s.business_id = i.business_id AND s.status = 'active'
			AND s.payments_receivable = TRUE AND s.primary_email_confirmed = TRUE AND s.payment_permission = TRUE
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > UTC_TIMESTAMP() FOR UPDATE`, hashToken(token)).
		Scan(&payment.BusinessID, &payment.InvoiceID, &payment.Reference, &payment.Currency, &payment.Amount, &status, &payment.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return PayPalPayment{}, ErrNotFound
	}
	if err != nil {
		return PayPalPayment{}, fmt.Errorf("lock invoice for PayPal payment: %w", err)
	}
	if status != "open" {
		return PayPalPayment{}, ErrInvoiceNotPayable
	}
	err = tx.QueryRowContext(ctx, `SELECT id, COALESCE(paypal_order_id, ''), COALESCE(approval_url, ''),
		create_request_id, capture_request_id, COALESCE(capture_id, ''), status, seller_merchant_id
		FROM invoice_paypal_payments WHERE business_id = ? AND invoice_id = ?
			AND status IN ('creating', 'created', 'capturing', 'failed') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, payment.BusinessID, payment.InvoiceID).
		Scan(&payment.ID, &payment.OrderID, &payment.ApprovalURL, &payment.CreateRequestID, &payment.CaptureRequestID, &payment.CaptureID, &payment.Status, &payment.MerchantID)
	if err == nil {
		if payment.Status == "created" {
			if err := tx.Commit(); err != nil {
				return PayPalPayment{}, fmt.Errorf("commit existing PayPal payment: %w", err)
			}
			return payment, nil
		}
		if payment.Status == "failed" {
			if _, err := tx.ExecContext(ctx, `UPDATE invoice_paypal_payments SET status = 'creating' WHERE id = ? AND status = 'failed'`, payment.ID); err != nil {
				return PayPalPayment{}, fmt.Errorf("retry PayPal order: %w", err)
			}
			payment.Status = "creating"
			if err := tx.Commit(); err != nil {
				return PayPalPayment{}, fmt.Errorf("commit PayPal order retry: %w", err)
			}
			return payment, nil
		}
		return PayPalPayment{}, ErrPaymentInProgress
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PayPalPayment{}, fmt.Errorf("find existing PayPal payment: %w", err)
	}
	payment.ID, err = randomID()
	if err != nil {
		return PayPalPayment{}, err
	}
	payment.CreateRequestID, err = randomID()
	if err != nil {
		return PayPalPayment{}, err
	}
	payment.CaptureRequestID, err = randomID()
	if err != nil {
		return PayPalPayment{}, err
	}
	payment.Status = "creating"
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_paypal_payments
		(id, business_id, invoice_id, create_request_id, capture_request_id, seller_merchant_id, currency, amount, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'creating')`, payment.ID, payment.BusinessID, payment.InvoiceID,
		payment.CreateRequestID, payment.CaptureRequestID, payment.MerchantID, payment.Currency, payment.Amount); err != nil {
		return PayPalPayment{}, fmt.Errorf("reserve PayPal payment: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return PayPalPayment{}, fmt.Errorf("commit PayPal payment reservation: %w", err)
	}
	return payment, nil
}

func (db *DB) SavePayPalOrder(ctx context.Context, payment PayPalPayment) error {
	result, err := db.ExecContext(ctx, `UPDATE invoice_paypal_payments SET paypal_order_id = ?, approval_url = ?, status = 'created'
		WHERE id = ? AND business_id = ? AND status = 'creating'`, payment.OrderID, payment.ApprovalURL, payment.ID, payment.BusinessID)
	if err != nil {
		return fmt.Errorf("save PayPal order: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		if err != nil {
			return fmt.Errorf("check saved PayPal order: %w", err)
		}
		return ErrPaymentInProgress
	}
	return nil
}

func (db *DB) FailPayPalOrder(ctx context.Context, paymentID string) error {
	_, err := db.ExecContext(ctx, `UPDATE invoice_paypal_payments SET status = 'failed' WHERE id = ? AND status = 'creating'`, paymentID)
	if err != nil {
		return fmt.Errorf("mark PayPal order failed: %w", err)
	}
	return nil
}

func (db *DB) ReservePayPalCapture(ctx context.Context, token, orderID string) (PayPalPayment, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return PayPalPayment{}, fmt.Errorf("begin PayPal capture: %w", err)
	}
	defer tx.Rollback()
	var payment PayPalPayment
	err = tx.QueryRowContext(ctx, `SELECT p.id, p.business_id, p.invoice_id, p.paypal_order_id,
		p.approval_url, p.create_request_id, p.capture_request_id, COALESCE(p.capture_id, ''),
		p.currency, CAST(p.amount AS CHAR), p.status, p.seller_merchant_id
		FROM public_invoice_tokens t JOIN invoice_paypal_payments p ON p.business_id = t.business_id AND p.invoice_id = t.invoice_id
		JOIN paypal_seller_accounts s ON s.business_id = p.business_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > UTC_TIMESTAMP()
		AND p.paypal_order_id = ? AND s.status = 'active' FOR UPDATE`, hashToken(token), orderID).
		Scan(&payment.ID, &payment.BusinessID, &payment.InvoiceID, &payment.OrderID, &payment.ApprovalURL,
			&payment.CreateRequestID, &payment.CaptureRequestID, &payment.CaptureID, &payment.Currency,
			&payment.Amount, &payment.Status, &payment.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return PayPalPayment{}, ErrNotFound
	}
	if err != nil {
		return PayPalPayment{}, fmt.Errorf("lock PayPal capture: %w", err)
	}
	if payment.Status == "completed" {
		if err := tx.Commit(); err != nil {
			return PayPalPayment{}, fmt.Errorf("commit completed PayPal capture: %w", err)
		}
		return payment, nil
	}
	if payment.Status != "created" {
		return PayPalPayment{}, ErrPaymentInProgress
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoice_paypal_payments SET status = 'capturing' WHERE id = ? AND status = 'created'`, payment.ID); err != nil {
		return PayPalPayment{}, fmt.Errorf("reserve PayPal capture: %w", err)
	}
	payment.Status = "capturing"
	if err := tx.Commit(); err != nil {
		return PayPalPayment{}, fmt.Errorf("commit PayPal capture reservation: %w", err)
	}
	return payment, nil
}

func (db *DB) ReleasePayPalCapture(ctx context.Context, paymentID string) error {
	_, err := db.ExecContext(ctx, `UPDATE invoice_paypal_payments SET status = 'created' WHERE id = ? AND status = 'capturing'`, paymentID)
	if err != nil {
		return fmt.Errorf("release PayPal capture: %w", err)
	}
	return nil
}

func (db *DB) CompletePayPalCapture(ctx context.Context, payment PayPalPayment, orderID, captureID, currency, amount string) error {
	return db.completePayPalCapture(ctx, payment.ID, payment.BusinessID, orderID, captureID, currency, amount)
}

func (db *DB) CompletePayPalWebhookCapture(ctx context.Context, orderID, captureID, currency, amount string) error {
	var paymentID, businessID string
	err := db.QueryRowContext(ctx, `SELECT id, business_id FROM invoice_paypal_payments WHERE paypal_order_id = ?`, orderID).Scan(&paymentID, &businessID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find PayPal webhook payment: %w", err)
	}
	return db.completePayPalCapture(ctx, paymentID, businessID, orderID, captureID, currency, amount)
}

func (db *DB) completePayPalCapture(ctx context.Context, paymentID, businessID, orderID, captureID, currency, amount string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PayPal payment completion: %w", err)
	}
	defer tx.Rollback()
	var invoiceID, invoiceStatus, paymentStatus, existingCaptureID, storedOrderID, storedCurrency, storedAmount string
	err = tx.QueryRowContext(ctx, `SELECT i.id, i.status, p.status, COALESCE(p.capture_id, ''), COALESCE(p.paypal_order_id, ''), p.currency, CAST(p.amount AS CHAR)
		FROM invoices i JOIN invoice_paypal_payments p ON p.business_id = i.business_id AND p.invoice_id = i.id
		WHERE p.id = ? AND p.business_id = ? FOR UPDATE`, paymentID, businessID).
		Scan(&invoiceID, &invoiceStatus, &paymentStatus, &existingCaptureID, &storedOrderID, &storedCurrency, &storedAmount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock invoice for PayPal completion: %w", err)
	}
	storedMinor, storedErr := billing.ParseMoney(storedAmount)
	actualMinor, actualErr := billing.ParseMoney(amount)
	if orderID == "" || orderID != storedOrderID || captureID == "" || currency != storedCurrency || storedErr != nil || actualErr != nil || actualMinor != storedMinor {
		return errors.New("PayPal capture does not match the reserved invoice payment")
	}
	if paymentStatus == "completed" && existingCaptureID == captureID && invoiceStatus == "paid" {
		return tx.Commit()
	}
	if (paymentStatus != "capturing" && paymentStatus != "created") || invoiceStatus != "open" {
		return ErrInvoiceNotPayable
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoice_paypal_payments SET status = 'completed', capture_id = ? WHERE id = ?`, captureID, paymentID); err != nil {
		return fmt.Errorf("save PayPal capture: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoices SET status = 'paid' WHERE business_id = ? AND id = ? AND status = 'open'`, businessID, invoiceID); err != nil {
		return fmt.Errorf("mark invoice paid: %w", err)
	}
	return tx.Commit()
}

func (db *DB) ClaimPayPalWebhook(ctx context.Context, eventID, eventType string) (bool, error) {
	result, err := db.ExecContext(ctx, `INSERT IGNORE INTO paypal_webhook_events (event_id, event_type) VALUES (?, ?)`, eventID, eventType)
	if err != nil {
		return false, fmt.Errorf("claim PayPal webhook: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check PayPal webhook claim: %w", err)
	}
	return rows == 1, nil
}

func (db *DB) FinishPayPalWebhook(ctx context.Context, eventID string) error {
	_, err := db.ExecContext(ctx, `UPDATE paypal_webhook_events SET processed_at = ? WHERE event_id = ?`, time.Now().UTC(), eventID)
	if err != nil {
		return fmt.Errorf("finish PayPal webhook: %w", err)
	}
	return nil
}

func (db *DB) ReleasePayPalWebhook(ctx context.Context, eventID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM paypal_webhook_events WHERE event_id = ? AND processed_at IS NULL`, eventID)
	if err != nil {
		return fmt.Errorf("release PayPal webhook: %w", err)
	}
	return nil
}
