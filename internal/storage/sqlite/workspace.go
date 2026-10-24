package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/billing"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

func (db *DB) CreateCategory(ctx context.Context, businessID, name string) (mysqlstore.Category, error) {
	item, err := db.DB.CreateCategory(ctx, businessID, name)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
		return mysqlstore.Category{}, mysqlstore.ErrDuplicateName
	}
	return item, err
}

func (db *DB) UpdateCategory(ctx context.Context, businessID, id, name string) (mysqlstore.Category, error) {
	item, err := db.DB.UpdateCategory(ctx, businessID, id, name)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
		return mysqlstore.Category{}, mysqlstore.ErrDuplicateName
	}
	return item, err
}

func (db *DB) CreateInvoice(ctx context.Context, businessID, clientID, publicBaseURL string, requested []mysqlstore.InvoiceItemInput) (mysqlstore.Invoice, error) {
	if len(requested) == 0 || len(requested) > 50 {
		return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
	}
	id, err := newID()
	if err != nil {
		return mysqlstore.Invoice{}, err
	}
	token, err := randomToken()
	if err != nil {
		return mysqlstore.Invoice{}, err
	}
	reference := "WP-" + strings.ToUpper(id[:10])
	createdAt := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return mysqlstore.Invoice{}, fmt.Errorf("begin invoice creation: %w", err)
	}
	defer tx.Rollback()
	var clientExists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM clients WHERE business_id = ? AND id = ?)", businessID, clientID).Scan(&clientExists); err != nil || !clientExists {
		return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
	}
	items := make([]mysqlstore.InvoiceItem, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	var subtotal int64
	currency := ""
	for _, input := range requested {
		if _, ok := seen[input.ProductID]; ok {
			return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
		}
		seen[input.ProductID] = struct{}{}
		var item mysqlstore.InvoiceItem
		var available bool
		var productCurrency string
		err := tx.QueryRowContext(ctx, `SELECT id, name, description, price, currency, available
			FROM catalog_products WHERE business_id = ? AND id = ?`, businessID, input.ProductID).
			Scan(&item.ProductID, &item.ProductName, &item.Description, &item.UnitPrice, &productCurrency, &available)
		if err != nil || !available || !billing.ValidateCurrency(productCurrency) {
			return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
		}
		if currency == "" {
			currency = productCurrency
		} else if currency != productCurrency {
			return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
		}
		unitPrice, err := billing.ParseMoney(item.UnitPrice)
		if err != nil {
			return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
		}
		lineTotal, nextSubtotal, err := billing.AddLine(subtotal, unitPrice, input.Quantity)
		if err != nil {
			return mysqlstore.Invoice{}, mysqlstore.ErrInvalidInvoice
		}
		subtotal = nextSubtotal
		item.ID, err = newID()
		if err != nil {
			return mysqlstore.Invoice{}, err
		}
		item.Quantity = input.Quantity
		item.UnitPrice = billing.FormatMoney(unitPrice)
		item.LineTotal = billing.FormatMoney(lineTotal)
		items = append(items, item)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoices (id, business_id, client_id, invoice_reference, currency, subtotal, total, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'open')`, id, businessID, clientID, reference, currency, billing.FormatMoney(subtotal), billing.FormatMoney(subtotal)); err != nil {
		return mysqlstore.Invoice{}, fmt.Errorf("insert invoice: %w", err)
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_items
			(id, business_id, invoice_id, product_id, product_name_snapshot, description_snapshot, quantity, unit_price, line_total)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, businessID, id, item.ProductID, item.ProductName, item.Description, item.Quantity, item.UnitPrice, item.LineTotal); err != nil {
			return mysqlstore.Invoice{}, fmt.Errorf("insert invoice item: %w", err)
		}
	}
	expiresAt := createdAt.Add(90 * 24 * time.Hour)
	if _, err := tx.ExecContext(ctx, `INSERT INTO public_invoice_tokens (token_hash, business_id, invoice_id, expires_at) VALUES (?, ?, ?, ?)`, hash(token), businessID, id, expiresAt); err != nil {
		return mysqlstore.Invoice{}, fmt.Errorf("insert public invoice token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return mysqlstore.Invoice{}, fmt.Errorf("commit invoice: %w", err)
	}
	return mysqlstore.Invoice{ID: id, Reference: reference, ClientID: clientID, Currency: currency,
		Subtotal: billing.FormatMoney(subtotal), Total: billing.FormatMoney(subtotal), Status: "open", CreatedAt: createdAt,
		Items: items, PublicURL: strings.TrimRight(publicBaseURL, "/") + "/i/" + token, PublicToken: token}, nil
}

func (db *DB) GetPublicInvoice(ctx context.Context, token string) (mysqlstore.PublicInvoice, error) {
	var invoice mysqlstore.PublicInvoice
	var businessID, invoiceID string
	err := db.QueryRowContext(ctx, `SELECT t.business_id, t.invoice_id, i.invoice_reference, b.name, i.currency, i.subtotal, i.total,
		i.status, i.created_at, t.expires_at FROM public_invoice_tokens t
		JOIN invoices i ON i.business_id = t.business_id AND i.id = t.invoice_id
		JOIN businesses b ON b.id = i.business_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > ?`, hash(token), time.Now().UTC()).
		Scan(&businessID, &invoiceID, &invoice.Reference, &invoice.Business, &invoice.Currency, &invoice.Subtotal, &invoice.Total, &invoice.Status, &invoice.CreatedAt, &invoice.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.PublicInvoice{}, mysqlstore.ErrNotFound
	}
	if err != nil {
		return mysqlstore.PublicInvoice{}, err
	}
	invoice.Items, err = db.loadInvoiceItems(ctx, businessID, invoiceID)
	return invoice, err
}

func (db *DB) CreatePublicInvoiceLink(ctx context.Context, businessID, invoiceID, publicBaseURL string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM invoices WHERE business_id = ? AND id = ?)", businessID, invoiceID).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "", mysqlstore.ErrNotFound
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, "UPDATE public_invoice_tokens SET revoked_at = ? WHERE business_id = ? AND invoice_id = ? AND revoked_at IS NULL", now, businessID, invoiceID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO public_invoice_tokens (token_hash, business_id, invoice_id, expires_at) VALUES (?, ?, ?, ?)", hash(token), businessID, invoiceID, now.Add(90*24*time.Hour)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return strings.TrimRight(publicBaseURL, "/") + "/i/" + token, nil
}

func (db *DB) RevokePublicInvoiceLink(ctx context.Context, businessID, invoiceID string) error {
	result, err := db.ExecContext(ctx, "UPDATE public_invoice_tokens SET revoked_at = ? WHERE business_id = ? AND invoice_id = ? AND revoked_at IS NULL", time.Now().UTC(), businessID, invoiceID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM invoices WHERE business_id = ? AND id = ?)", businessID, invoiceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return mysqlstore.ErrNotFound
		}
	}
	return nil
}

func (db *DB) loadInvoiceItems(ctx context.Context, businessID, invoiceID string) ([]mysqlstore.InvoiceItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, product_id, product_name_snapshot, description_snapshot, quantity,
		unit_price, line_total FROM invoice_items WHERE business_id = ? AND invoice_id = ? ORDER BY id`, businessID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mysqlstore.InvoiceItem, 0)
	for rows.Next() {
		var item mysqlstore.InvoiceItem
		var productID sql.NullString
		if err := rows.Scan(&item.ID, &productID, &item.ProductName, &item.Description, &item.Quantity, &item.UnitPrice, &item.LineTotal); err != nil {
			return nil, err
		}
		if productID.Valid {
			item.ProductID = productID.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
