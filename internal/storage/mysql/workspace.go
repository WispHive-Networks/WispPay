package mysql

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
	mysqldriver "github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound        = errors.New("record not found")
	ErrDuplicateName   = errors.New("name already exists")
	ErrInvalidCategory = errors.New("category does not belong to business")
	ErrInvalidInvoice  = errors.New("invoice references invalid or unavailable data")
)

type Category struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Product struct {
	ID          string    `json:"id"`
	CategoryID  *string   `json:"categoryId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       string    `json:"price"`
	Currency    string    `json:"currency"`
	Available   bool      `json:"available"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Client struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Email       *string   `json:"email,omitempty"`
	Phone       *string   `json:"phone,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type InvoiceItemInput struct {
	ProductID string
	Quantity  int
}

type InvoiceItem struct {
	ID          string `json:"id,omitempty"`
	ProductID   string `json:"productId,omitempty"`
	ProductName string `json:"productName"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
	LineTotal   string `json:"lineTotal"`
}

type Invoice struct {
	ID          string        `json:"id"`
	Reference   string        `json:"reference"`
	ClientID    string        `json:"clientId,omitempty"`
	ClientName  string        `json:"clientName,omitempty"`
	Currency    string        `json:"currency"`
	Subtotal    string        `json:"subtotal"`
	Total       string        `json:"total"`
	Status      string        `json:"status"`
	CreatedAt   time.Time     `json:"createdAt"`
	Items       []InvoiceItem `json:"items,omitempty"`
	PublicURL   string        `json:"publicUrl,omitempty"`
	PublicToken string        `json:"-"`
}

type PublicInvoice struct {
	Reference string        `json:"reference"`
	Business  string        `json:"businessName"`
	Currency  string        `json:"currency"`
	Subtotal  string        `json:"subtotal"`
	Total     string        `json:"total"`
	Status    string        `json:"status"`
	CreatedAt time.Time     `json:"createdAt"`
	ExpiresAt time.Time     `json:"expiresAt"`
	Items     []InvoiceItem `json:"items"`
}

func (db *DB) ListCategories(ctx context.Context, businessID string) ([]Category, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, created_at, updated_at FROM catalog_categories
		WHERE business_id = ? ORDER BY name LIMIT 200`, businessID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	items := make([]Category, 0)
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.Name, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) CreateCategory(ctx context.Context, businessID, name string) (Category, error) {
	id, err := randomID()
	if err != nil {
		return Category{}, err
	}
	_, err = db.ExecContext(ctx, "INSERT INTO catalog_categories (id, business_id, name) VALUES (?, ?, ?)", id, businessID, name)
	if err != nil {
		return Category{}, duplicateOrError(err, "create category")
	}
	return db.GetCategory(ctx, businessID, id)
}

func (db *DB) GetCategory(ctx context.Context, businessID, id string) (Category, error) {
	var item Category
	err := db.QueryRowContext(ctx, `SELECT id, name, created_at, updated_at FROM catalog_categories
		WHERE business_id = ? AND id = ?`, businessID, id).Scan(&item.ID, &item.Name, &item.CreatedAt, &item.UpdatedAt)
	return item, notFoundOrError(err, "get category")
}

func (db *DB) UpdateCategory(ctx context.Context, businessID, id, name string) (Category, error) {
	result, err := db.ExecContext(ctx, "UPDATE catalog_categories SET name = ? WHERE business_id = ? AND id = ?", name, businessID, id)
	if err != nil {
		return Category{}, duplicateOrError(err, "update category")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Category{}, fmt.Errorf("check category update: %w", err)
	}
	if affected == 0 && !businessRecordExists(ctx, db.DB, "catalog_categories", businessID, id) {
		return Category{}, ErrNotFound
	}
	return db.GetCategory(ctx, businessID, id)
}

func (db *DB) ListProducts(ctx context.Context, businessID string) ([]Product, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, category_id, name, description, CAST(price AS CHAR), currency, available, created_at, updated_at
		FROM catalog_products WHERE business_id = ? ORDER BY created_at DESC LIMIT 200`, businessID)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	items := make([]Product, 0)
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) CreateProduct(ctx context.Context, businessID string, input Product) (Product, error) {
	if input.CategoryID != nil {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM catalog_categories WHERE business_id = ? AND id = ?)", businessID, *input.CategoryID).Scan(&exists); err != nil {
			return Product{}, fmt.Errorf("validate product category: %w", err)
		}
		if !exists {
			return Product{}, ErrInvalidCategory
		}
	}
	id, err := randomID()
	if err != nil {
		return Product{}, err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO catalog_products
		(id, business_id, category_id, name, description, price, currency, available) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, businessID, input.CategoryID, input.Name, input.Description, input.Price, input.Currency, input.Available)
	if err != nil {
		return Product{}, fmt.Errorf("create product: %w", err)
	}
	return db.GetProduct(ctx, businessID, id)
}

func (db *DB) GetProduct(ctx context.Context, businessID, id string) (Product, error) {
	row := db.QueryRowContext(ctx, `SELECT id, category_id, name, description, CAST(price AS CHAR), currency, available, created_at, updated_at
		FROM catalog_products WHERE business_id = ? AND id = ?`, businessID, id)
	item, err := scanProduct(row)
	return item, notFoundOrError(err, "get product")
}

func (db *DB) UpdateProduct(ctx context.Context, businessID, id string, input Product) (Product, error) {
	if input.CategoryID != nil {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM catalog_categories WHERE business_id = ? AND id = ?)", businessID, *input.CategoryID).Scan(&exists); err != nil {
			return Product{}, fmt.Errorf("validate product category: %w", err)
		}
		if !exists {
			return Product{}, ErrInvalidCategory
		}
	}
	result, err := db.ExecContext(ctx, `UPDATE catalog_products SET category_id = ?, name = ?, description = ?, price = ?, currency = ?, available = ?
		WHERE business_id = ? AND id = ?`, input.CategoryID, input.Name, input.Description, input.Price, input.Currency, input.Available, businessID, id)
	if err != nil {
		return Product{}, fmt.Errorf("update product: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Product{}, fmt.Errorf("check product update: %w", err)
	}
	if affected == 0 && !businessRecordExists(ctx, db.DB, "catalog_products", businessID, id) {
		return Product{}, ErrNotFound
	}
	return db.GetProduct(ctx, businessID, id)
}

func (db *DB) ListClients(ctx context.Context, businessID string) ([]Client, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, display_name, email, phone, created_at, updated_at FROM clients
		WHERE business_id = ? ORDER BY created_at DESC LIMIT 200`, businessID)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	defer rows.Close()
	items := make([]Client, 0)
	for rows.Next() {
		var item Client
		if err := rows.Scan(&item.ID, &item.DisplayName, &item.Email, &item.Phone, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan client: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) CreateClient(ctx context.Context, businessID string, input Client) (Client, error) {
	id, err := randomID()
	if err != nil {
		return Client{}, err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO clients (id, business_id, display_name, email, phone) VALUES (?, ?, ?, ?, ?)`,
		id, businessID, input.DisplayName, input.Email, input.Phone)
	if err != nil {
		return Client{}, fmt.Errorf("create client: %w", err)
	}
	return db.GetClient(ctx, businessID, id)
}

func (db *DB) GetClient(ctx context.Context, businessID, id string) (Client, error) {
	var item Client
	err := db.QueryRowContext(ctx, `SELECT id, display_name, email, phone, created_at, updated_at FROM clients
		WHERE business_id = ? AND id = ?`, businessID, id).Scan(&item.ID, &item.DisplayName, &item.Email, &item.Phone, &item.CreatedAt, &item.UpdatedAt)
	return item, notFoundOrError(err, "get client")
}

func (db *DB) UpdateClient(ctx context.Context, businessID, id string, input Client) (Client, error) {
	result, err := db.ExecContext(ctx, `UPDATE clients SET display_name = ?, email = ?, phone = ? WHERE business_id = ? AND id = ?`,
		input.DisplayName, input.Email, input.Phone, businessID, id)
	if err != nil {
		return Client{}, fmt.Errorf("update client: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Client{}, fmt.Errorf("check client update: %w", err)
	}
	if affected == 0 && !businessRecordExists(ctx, db.DB, "clients", businessID, id) {
		return Client{}, ErrNotFound
	}
	return db.GetClient(ctx, businessID, id)
}

func (db *DB) CreateInvoice(ctx context.Context, businessID, clientID, publicBaseURL string, requested []InvoiceItemInput) (Invoice, error) {
	if len(requested) == 0 || len(requested) > 50 {
		return Invoice{}, ErrInvalidInvoice
	}
	seen := make(map[string]struct{}, len(requested))
	id, err := randomID()
	if err != nil {
		return Invoice{}, err
	}
	token, err := randomTokenBytes()
	if err != nil {
		return Invoice{}, err
	}
	reference := "WP-" + strings.ToUpper(id[:10])
	expiresAt := time.Now().UTC().Add(90 * 24 * time.Hour)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Invoice{}, fmt.Errorf("begin invoice creation: %w", err)
	}
	defer tx.Rollback()
	var clientExists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM clients WHERE business_id = ? AND id = ?)", businessID, clientID).Scan(&clientExists); err != nil || !clientExists {
		return Invoice{}, ErrInvalidInvoice
	}
	items := make([]InvoiceItem, 0, len(requested))
	var subtotal int64
	currency := ""
	for _, request := range requested {
		if _, exists := seen[request.ProductID]; exists {
			return Invoice{}, ErrInvalidInvoice
		}
		seen[request.ProductID] = struct{}{}
		var productID, name, description, priceText, productCurrency string
		var available bool
		err := tx.QueryRowContext(ctx, `SELECT id, name, description, CAST(price AS CHAR), currency, available
			FROM catalog_products WHERE business_id = ? AND id = ? FOR UPDATE`, businessID, request.ProductID).
			Scan(&productID, &name, &description, &priceText, &productCurrency, &available)
		if err != nil || !available || !billing.ValidateCurrency(productCurrency) {
			return Invoice{}, ErrInvalidInvoice
		}
		if currency == "" {
			currency = productCurrency
		} else if currency != productCurrency {
			return Invoice{}, ErrInvalidInvoice
		}
		unitPrice, err := billing.ParseMoney(priceText)
		if err != nil {
			return Invoice{}, ErrInvalidInvoice
		}
		lineTotal, nextSubtotal, err := billing.AddLine(subtotal, unitPrice, request.Quantity)
		if err != nil {
			return Invoice{}, ErrInvalidInvoice
		}
		subtotal = nextSubtotal
		itemID, err := randomID()
		if err != nil {
			return Invoice{}, err
		}
		items = append(items, InvoiceItem{ID: itemID, ProductID: productID, ProductName: name, Description: description,
			Quantity: request.Quantity, UnitPrice: billing.FormatMoney(unitPrice), LineTotal: billing.FormatMoney(lineTotal)})
	}
	if currency == "" {
		return Invoice{}, ErrInvalidInvoice
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoices (id, business_id, client_id, invoice_reference, currency, subtotal, total, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'open')`, id, businessID, clientID, reference, currency, billing.FormatMoney(subtotal), billing.FormatMoney(subtotal)); err != nil {
		return Invoice{}, fmt.Errorf("insert invoice: %w", err)
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_items
			(id, business_id, invoice_id, product_id, product_name_snapshot, description_snapshot, quantity, unit_price, line_total)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, businessID, id, item.ProductID, item.ProductName, item.Description, item.Quantity, item.UnitPrice, item.LineTotal); err != nil {
			return Invoice{}, fmt.Errorf("insert invoice item: %w", err)
		}
	}
	tokenHash := hashToken(token)
	if _, err := tx.ExecContext(ctx, `INSERT INTO public_invoice_tokens (token_hash, business_id, invoice_id, expires_at)
		VALUES (?, ?, ?, ?)`, tokenHash, businessID, id, expiresAt); err != nil {
		return Invoice{}, fmt.Errorf("insert public invoice token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Invoice{}, fmt.Errorf("commit invoice: %w", err)
	}
	base := strings.TrimRight(publicBaseURL, "/")
	return Invoice{ID: id, Reference: reference, ClientID: clientID, Currency: currency,
		Subtotal: billing.FormatMoney(subtotal), Total: billing.FormatMoney(subtotal), Status: "open",
		CreatedAt: time.Now().UTC(), Items: items, PublicURL: base + "/i/" + token, PublicToken: token}, nil
}

func (db *DB) ListInvoices(ctx context.Context, businessID string) ([]Invoice, error) {
	rows, err := db.QueryContext(ctx, `SELECT i.id, i.invoice_reference, i.client_id, c.display_name, i.currency,
		CAST(i.subtotal AS CHAR), CAST(i.total AS CHAR), i.status, i.created_at FROM invoices i
		JOIN clients c ON c.business_id = i.business_id AND c.id = i.client_id
		WHERE i.business_id = ? ORDER BY i.created_at DESC LIMIT 200`, businessID)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()
	items := make([]Invoice, 0)
	for rows.Next() {
		var item Invoice
		if err := rows.Scan(&item.ID, &item.Reference, &item.ClientID, &item.ClientName, &item.Currency, &item.Subtotal, &item.Total, &item.Status, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) GetInvoice(ctx context.Context, businessID, id string) (Invoice, error) {
	var item Invoice
	err := db.QueryRowContext(ctx, `SELECT i.id, i.invoice_reference, i.client_id, c.display_name, i.currency,
		CAST(i.subtotal AS CHAR), CAST(i.total AS CHAR), i.status, i.created_at FROM invoices i
		JOIN clients c ON c.business_id = i.business_id AND c.id = i.client_id
		WHERE i.business_id = ? AND i.id = ?`, businessID, id).
		Scan(&item.ID, &item.Reference, &item.ClientID, &item.ClientName, &item.Currency, &item.Subtotal, &item.Total, &item.Status, &item.CreatedAt)
	if err != nil {
		return Invoice{}, notFoundOrError(err, "get invoice")
	}
	items, err := db.loadInvoiceItems(ctx, businessID, id)
	if err != nil {
		return Invoice{}, err
	}
	item.Items = items
	return item, nil
}

func (db *DB) GetPublicInvoice(ctx context.Context, token string) (PublicInvoice, error) {
	var invoice PublicInvoice
	var businessID, invoiceID string
	err := db.QueryRowContext(ctx, `SELECT t.business_id, t.invoice_id, i.invoice_reference, b.name, i.currency, CAST(i.subtotal AS CHAR), CAST(i.total AS CHAR),
		i.status, i.created_at, t.expires_at FROM public_invoice_tokens t
		JOIN invoices i ON i.business_id = t.business_id AND i.id = t.invoice_id
		JOIN businesses b ON b.id = i.business_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > UTC_TIMESTAMP()`, hashToken(token)).
		Scan(&businessID, &invoiceID, &invoice.Reference, &invoice.Business, &invoice.Currency, &invoice.Subtotal, &invoice.Total, &invoice.Status, &invoice.CreatedAt, &invoice.ExpiresAt)
	if err != nil {
		return PublicInvoice{}, notFoundOrError(err, "get public invoice")
	}
	invoice.Items, err = db.loadInvoiceItems(ctx, businessID, invoiceID)
	if err != nil {
		return PublicInvoice{}, err
	}
	return invoice, nil
}

func (db *DB) CreatePublicInvoiceLink(ctx context.Context, businessID, invoiceID, publicBaseURL string) (string, error) {
	token, err := randomTokenBytes()
	if err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin public link update: %w", err)
	}
	defer tx.Rollback()
	var lockedInvoiceID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM invoices WHERE business_id = ? AND id = ? FOR UPDATE", businessID, invoiceID).Scan(&lockedInvoiceID); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("lock invoice for public link update: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public_invoice_tokens SET revoked_at = UTC_TIMESTAMP()
		WHERE business_id = ? AND invoice_id = ? AND revoked_at IS NULL`, businessID, invoiceID); err != nil {
		return "", fmt.Errorf("revoke previous public invoice link: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public_invoice_tokens (token_hash, business_id, invoice_id, expires_at)
		VALUES (?, ?, ?, UTC_TIMESTAMP() + INTERVAL 90 DAY)`, hashToken(token), businessID, invoiceID); err != nil {
		return "", fmt.Errorf("create public invoice link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit public link update: %w", err)
	}
	return strings.TrimRight(publicBaseURL, "/") + "/i/" + token, nil
}

func (db *DB) RevokePublicInvoiceLink(ctx context.Context, businessID, invoiceID string) error {
	result, err := db.ExecContext(ctx, `UPDATE public_invoice_tokens SET revoked_at = UTC_TIMESTAMP()
		WHERE business_id = ? AND invoice_id = ? AND revoked_at IS NULL`, businessID, invoiceID)
	if err != nil {
		return fmt.Errorf("revoke public invoice link: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check public invoice link revocation: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM invoices WHERE business_id = ? AND id = ?)", businessID, invoiceID).Scan(&exists); err != nil {
			return fmt.Errorf("check invoice for link revocation: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (db *DB) loadInvoiceItems(ctx context.Context, businessID, invoiceID string) ([]InvoiceItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, product_id, product_name_snapshot, description_snapshot, quantity,
		CAST(unit_price AS CHAR), CAST(line_total AS CHAR) FROM invoice_items WHERE business_id = ? AND invoice_id = ? ORDER BY id`, businessID, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("list invoice items: %w", err)
	}
	defer rows.Close()
	items := make([]InvoiceItem, 0)
	for rows.Next() {
		var item InvoiceItem
		var productID sql.NullString
		if err := rows.Scan(&item.ID, &productID, &item.ProductName, &item.Description, &item.Quantity, &item.UnitPrice, &item.LineTotal); err != nil {
			return nil, fmt.Errorf("scan invoice item: %w", err)
		}
		if productID.Valid {
			item.ProductID = productID.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanProduct(row rowScanner) (Product, error) {
	var item Product
	var categoryID sql.NullString
	err := row.Scan(&item.ID, &categoryID, &item.Name, &item.Description, &item.Price, &item.Currency, &item.Available, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Product{}, err
	}
	if categoryID.Valid {
		item.CategoryID = &categoryID.String
	}
	return item, nil
}

func notFoundOrError(err error, operation string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func businessRecordExists(ctx context.Context, db *sql.DB, table, businessID, id string) bool {
	queries := map[string]string{
		"catalog_categories": "SELECT EXISTS(SELECT 1 FROM catalog_categories WHERE business_id = ? AND id = ?)",
		"catalog_products":   "SELECT EXISTS(SELECT 1 FROM catalog_products WHERE business_id = ? AND id = ?)",
		"clients":            "SELECT EXISTS(SELECT 1 FROM clients WHERE business_id = ? AND id = ?)",
	}
	query, ok := queries[table]
	if !ok {
		return false
	}
	var exists bool
	return db.QueryRowContext(ctx, query, businessID, id).Scan(&exists) == nil && exists
}

func duplicateOrError(err error, operation string) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrDuplicateName
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func randomTokenBytes() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate public invoice token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}
