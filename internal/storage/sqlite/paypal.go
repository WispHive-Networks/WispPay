package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/billing"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

func (db *DB) BeginPayPalSellerOnboarding(ctx context.Context, businessID, stateHash string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO paypal_seller_accounts (business_id, tracking_id, onboarding_state_hash, status)
		VALUES (?, ?, ?, 'pending') ON CONFLICT(business_id) DO UPDATE SET
		onboarding_state_hash = CASE WHEN paypal_seller_accounts.status = 'active' THEN paypal_seller_accounts.onboarding_state_hash ELSE excluded.onboarding_state_hash END,
		status = CASE WHEN paypal_seller_accounts.status = 'active' THEN paypal_seller_accounts.status ELSE 'pending' END`, businessID, businessID, stateHash)
	return err
}

func (db *DB) GetPayPalSeller(ctx context.Context, businessID string) (mysqlstore.PayPalSeller, error) {
	var seller mysqlstore.PayPalSeller
	err := db.QueryRowContext(ctx, `SELECT COALESCE(merchant_id, ''), status, payments_receivable,
		primary_email_confirmed, payment_permission FROM paypal_seller_accounts WHERE business_id = ?`, businessID).
		Scan(&seller.MerchantID, &seller.Status, &seller.PaymentsReceivable, &seller.EmailConfirmed, &seller.PaymentPermission)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.PayPalSeller{}, mysqlstore.ErrSellerNotConnected
	}
	return seller, err
}

func (db *DB) CompletePayPalSellerOnboarding(ctx context.Context, businessID, stateHash, merchantID string, paymentsReceivable, emailConfirmed, paymentPermission bool) error {
	active := paymentsReceivable && emailConfirmed && paymentPermission
	status := "pending"
	var connectedAt any
	if active {
		status = "active"
		connectedAt = time.Now().UTC()
	}
	result, err := db.ExecContext(ctx, `UPDATE paypal_seller_accounts SET merchant_id = ?, payments_receivable = ?,
		primary_email_confirmed = ?, payment_permission = ?, status = ?, connected_at = ?, onboarding_state_hash = NULL
		WHERE business_id = ? AND tracking_id = ? AND onboarding_state_hash = ?`, merchantID, paymentsReceivable,
		emailConfirmed, paymentPermission, status, connectedAt, businessID, businessID, stateHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return mysqlstore.ErrSellerStateExpired
	}
	return nil
}

func (db *DB) RevokePayPalSeller(ctx context.Context, merchantID string) error {
	_, err := db.ExecContext(ctx, `UPDATE paypal_seller_accounts SET status = 'revoked', payments_receivable = 0,
		payment_permission = 0 WHERE merchant_id = ?`, merchantID)
	return err
}

func (db *DB) GetPublicInvoicePayPal(ctx context.Context, token string) (mysqlstore.PayPalInvoice, error) {
	var invoice mysqlstore.PayPalInvoice
	err := db.QueryRowContext(ctx, `SELECT i.business_id, i.id, i.invoice_reference, i.currency, i.total, i.status, COALESCE(s.merchant_id, '')
		FROM public_invoice_tokens t JOIN invoices i ON i.business_id = t.business_id AND i.id = t.invoice_id
		LEFT JOIN paypal_seller_accounts s ON s.business_id = i.business_id AND s.status = 'active'
			AND s.payments_receivable = 1 AND s.primary_email_confirmed = 1 AND s.payment_permission = 1
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > ?`, hash(token), time.Now().UTC()).
		Scan(&invoice.BusinessID, &invoice.InvoiceID, &invoice.Reference, &invoice.Currency, &invoice.Amount, &invoice.Status, &invoice.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.PayPalInvoice{}, mysqlstore.ErrNotFound
	}
	if err != nil {
		return mysqlstore.PayPalInvoice{}, err
	}
	if invoice.MerchantID == "" {
		return mysqlstore.PayPalInvoice{}, mysqlstore.ErrSellerNotConnected
	}
	return invoice, nil
}

func (db *DB) StartPayPalPayment(ctx context.Context, token string) (mysqlstore.PayPalPayment, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	defer tx.Rollback()
	var payment mysqlstore.PayPalPayment
	var invoiceStatus string
	err = tx.QueryRowContext(ctx, `SELECT i.business_id, i.id, i.invoice_reference, i.currency, i.total, i.status, s.merchant_id
		FROM public_invoice_tokens t JOIN invoices i ON i.business_id = t.business_id AND i.id = t.invoice_id
		JOIN paypal_seller_accounts s ON s.business_id = i.business_id AND s.status = 'active'
			AND s.payments_receivable = 1 AND s.primary_email_confirmed = 1 AND s.payment_permission = 1
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > ?`, hash(token), time.Now().UTC()).
		Scan(&payment.BusinessID, &payment.InvoiceID, &payment.Reference, &payment.Currency, &payment.Amount, &invoiceStatus, &payment.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.PayPalPayment{}, mysqlstore.ErrNotFound
	}
	if err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	if invoiceStatus != "open" {
		return mysqlstore.PayPalPayment{}, mysqlstore.ErrInvoiceNotPayable
	}
	err = tx.QueryRowContext(ctx, `SELECT id, COALESCE(paypal_order_id, ''), COALESCE(approval_url, ''), create_request_id,
		capture_request_id, COALESCE(capture_id, ''), status, seller_merchant_id FROM invoice_paypal_payments
		WHERE business_id = ? AND invoice_id = ? AND status IN ('creating', 'created', 'capturing', 'failed')
		ORDER BY created_at DESC LIMIT 1`, payment.BusinessID, payment.InvoiceID).
		Scan(&payment.ID, &payment.OrderID, &payment.ApprovalURL, &payment.CreateRequestID, &payment.CaptureRequestID, &payment.CaptureID, &payment.Status, &payment.MerchantID)
	if err == nil {
		switch payment.Status {
		case "created":
			if err := tx.Commit(); err != nil {
				return mysqlstore.PayPalPayment{}, err
			}
			return payment, nil
		case "failed":
			if _, err := tx.ExecContext(ctx, "UPDATE invoice_paypal_payments SET status = 'creating' WHERE id = ? AND status = 'failed'", payment.ID); err != nil {
				return mysqlstore.PayPalPayment{}, err
			}
			payment.Status = "creating"
			if err := tx.Commit(); err != nil {
				return mysqlstore.PayPalPayment{}, err
			}
			return payment, nil
		default:
			return mysqlstore.PayPalPayment{}, mysqlstore.ErrPaymentInProgress
		}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.PayPalPayment{}, err
	}
	if payment.ID, err = newID(); err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	if payment.CreateRequestID, err = newID(); err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	if payment.CaptureRequestID, err = newID(); err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	payment.Status = "creating"
	_, err = tx.ExecContext(ctx, `INSERT INTO invoice_paypal_payments
		(id, business_id, invoice_id, create_request_id, capture_request_id, seller_merchant_id, currency, amount, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'creating')`, payment.ID, payment.BusinessID, payment.InvoiceID,
		payment.CreateRequestID, payment.CaptureRequestID, payment.MerchantID, payment.Currency, payment.Amount)
	if err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	if err := tx.Commit(); err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	return payment, nil
}

func (db *DB) SavePayPalOrder(ctx context.Context, payment mysqlstore.PayPalPayment) error {
	result, err := db.ExecContext(ctx, `UPDATE invoice_paypal_payments SET paypal_order_id = ?, approval_url = ?, status = 'created'
		WHERE id = ? AND business_id = ? AND status = 'creating'`, payment.OrderID, payment.ApprovalURL, payment.ID, payment.BusinessID)
	return requireOneRow(result, err)
}

func (db *DB) FailPayPalOrder(ctx context.Context, paymentID string) error {
	_, err := db.ExecContext(ctx, "UPDATE invoice_paypal_payments SET status = 'failed' WHERE id = ? AND status = 'creating'", paymentID)
	return err
}

func (db *DB) ReservePayPalCapture(ctx context.Context, token, orderID string) (mysqlstore.PayPalPayment, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	defer tx.Rollback()
	var payment mysqlstore.PayPalPayment
	err = tx.QueryRowContext(ctx, `SELECT p.id, p.business_id, p.invoice_id, p.paypal_order_id, p.approval_url,
		p.create_request_id, p.capture_request_id, COALESCE(p.capture_id, ''), p.currency, p.amount, p.status, p.seller_merchant_id
		FROM public_invoice_tokens t JOIN invoice_paypal_payments p ON p.business_id = t.business_id AND p.invoice_id = t.invoice_id
		JOIN paypal_seller_accounts s ON s.business_id = p.business_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > ? AND p.paypal_order_id = ? AND s.status = 'active'`,
		hash(token), time.Now().UTC(), orderID).
		Scan(&payment.ID, &payment.BusinessID, &payment.InvoiceID, &payment.OrderID, &payment.ApprovalURL,
			&payment.CreateRequestID, &payment.CaptureRequestID, &payment.CaptureID, &payment.Currency,
			&payment.Amount, &payment.Status, &payment.MerchantID)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.PayPalPayment{}, mysqlstore.ErrNotFound
	}
	if err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	if payment.Status == "completed" {
		return payment, tx.Commit()
	}
	if payment.Status != "created" {
		return mysqlstore.PayPalPayment{}, mysqlstore.ErrPaymentInProgress
	}
	if _, err := tx.ExecContext(ctx, "UPDATE invoice_paypal_payments SET status = 'capturing' WHERE id = ? AND status = 'created'", payment.ID); err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	if err := tx.Commit(); err != nil {
		return mysqlstore.PayPalPayment{}, err
	}
	payment.Status = "capturing"
	return payment, nil
}

func (db *DB) ReleasePayPalCapture(ctx context.Context, paymentID string) error {
	_, err := db.ExecContext(ctx, "UPDATE invoice_paypal_payments SET status = 'created' WHERE id = ? AND status = 'capturing'", paymentID)
	return err
}

func (db *DB) CompletePayPalCapture(ctx context.Context, payment mysqlstore.PayPalPayment, orderID, captureID, currency, amount string) error {
	return db.completeSQLitePayPalCapture(ctx, payment.ID, payment.BusinessID, orderID, captureID, currency, amount)
}

func (db *DB) CompletePayPalWebhookCapture(ctx context.Context, orderID, captureID, currency, amount string) error {
	var paymentID, businessID string
	err := db.QueryRowContext(ctx, "SELECT id, business_id FROM invoice_paypal_payments WHERE paypal_order_id = ?", orderID).Scan(&paymentID, &businessID)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.ErrNotFound
	}
	if err != nil {
		return err
	}
	return db.completeSQLitePayPalCapture(ctx, paymentID, businessID, orderID, captureID, currency, amount)
}

func (db *DB) completeSQLitePayPalCapture(ctx context.Context, paymentID, businessID, orderID, captureID, currency, amount string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var invoiceID, invoiceStatus, paymentStatus, existingCaptureID, storedOrderID, storedCurrency, storedAmount string
	err = tx.QueryRowContext(ctx, `SELECT i.id, i.status, p.status, COALESCE(p.capture_id, ''), COALESCE(p.paypal_order_id, ''), p.currency, p.amount
		FROM invoices i JOIN invoice_paypal_payments p ON p.business_id = i.business_id AND p.invoice_id = i.id
		WHERE p.id = ? AND p.business_id = ?`, paymentID, businessID).
		Scan(&invoiceID, &invoiceStatus, &paymentStatus, &existingCaptureID, &storedOrderID, &storedCurrency, &storedAmount)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.ErrNotFound
	}
	if err != nil {
		return err
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
		return mysqlstore.ErrInvoiceNotPayable
	}
	if _, err := tx.ExecContext(ctx, "UPDATE invoice_paypal_payments SET status = 'completed', capture_id = ? WHERE id = ?", captureID, paymentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE invoices SET status = 'paid' WHERE business_id = ? AND id = ? AND status = 'open'", businessID, invoiceID); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ClaimPayPalWebhook(ctx context.Context, eventID, eventType string) (bool, error) {
	result, err := db.ExecContext(ctx, "INSERT INTO paypal_webhook_events (event_id, event_type) VALUES (?, ?) ON CONFLICT(event_id) DO NOTHING", eventID, eventType)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (db *DB) FinishPayPalWebhook(ctx context.Context, eventID string) error {
	_, err := db.ExecContext(ctx, "UPDATE paypal_webhook_events SET processed_at = ? WHERE event_id = ?", time.Now().UTC(), eventID)
	return err
}

func (db *DB) ReleasePayPalWebhook(ctx context.Context, eventID string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM paypal_webhook_events WHERE event_id = ? AND processed_at IS NULL", eventID)
	return err
}

func requireOneRow(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("update PayPal state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return mysqlstore.ErrPaymentInProgress
	}
	return nil
}
