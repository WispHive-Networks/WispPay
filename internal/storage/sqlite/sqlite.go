package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
	_ "modernc.org/sqlite"
)

type DB struct {
	*mysqlstore.DB
}

func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("SQLITE_PATH is empty")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create SQLite data directory: %w", err)
		}
	}
	dsn := path
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve SQLite path: %w", err)
		}
		dsn = filepath.ToSlash(absolute)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable SQLite foreign keys: %w", err)
	}
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize SQLite schema statement %q: %w", strings.TrimSpace(statement), err)
		}
	}
	store := &mysqlstore.DB{DB: db}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Ready(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("check SQLite database: %w", err)
	}
	return &DB{DB: store}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS businesses (id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, business_id TEXT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, csrf_hash TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS catalog_categories (id TEXT PRIMARY KEY, business_id TEXT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE, name TEXT NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(business_id, name), UNIQUE(business_id, id));
CREATE TABLE IF NOT EXISTS catalog_products (id TEXT PRIMARY KEY, business_id TEXT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE, category_id TEXT, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', price TEXT NOT NULL, currency TEXT NOT NULL, available BOOLEAN NOT NULL DEFAULT 1, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(business_id, id), FOREIGN KEY(business_id, category_id) REFERENCES catalog_categories(business_id, id));
CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, business_id TEXT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE, display_name TEXT NOT NULL, email TEXT, phone TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(business_id, id));
CREATE TABLE IF NOT EXISTS invoices (id TEXT PRIMARY KEY, business_id TEXT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE, client_id TEXT NOT NULL, invoice_reference TEXT NOT NULL, currency TEXT NOT NULL, subtotal TEXT NOT NULL, total TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'open', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(business_id, invoice_reference), UNIQUE(business_id, id), FOREIGN KEY(business_id, client_id) REFERENCES clients(business_id, id));
CREATE TABLE IF NOT EXISTS invoice_items (id TEXT PRIMARY KEY, business_id TEXT NOT NULL, invoice_id TEXT NOT NULL, product_id TEXT, product_name_snapshot TEXT NOT NULL, description_snapshot TEXT NOT NULL, quantity INTEGER NOT NULL CHECK(quantity > 0), unit_price TEXT NOT NULL, line_total TEXT NOT NULL, FOREIGN KEY(business_id, invoice_id) REFERENCES invoices(business_id, id) ON DELETE CASCADE, FOREIGN KEY(business_id, product_id) REFERENCES catalog_products(business_id, id));
CREATE TABLE IF NOT EXISTS public_invoice_tokens (token_hash TEXT PRIMARY KEY, business_id TEXT NOT NULL, invoice_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(business_id, invoice_id) REFERENCES invoices(business_id, id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS paypal_seller_accounts (business_id TEXT PRIMARY KEY REFERENCES businesses(id) ON DELETE CASCADE, tracking_id TEXT NOT NULL UNIQUE, onboarding_state_hash TEXT, merchant_id TEXT UNIQUE, status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'active', 'revoked')), payments_receivable BOOLEAN NOT NULL DEFAULT 0, primary_email_confirmed BOOLEAN NOT NULL DEFAULT 0, payment_permission BOOLEAN NOT NULL DEFAULT 0, connected_at DATETIME, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS invoice_paypal_payments (id TEXT PRIMARY KEY, business_id TEXT NOT NULL, invoice_id TEXT NOT NULL, paypal_order_id TEXT, approval_url TEXT, create_request_id TEXT NOT NULL, capture_request_id TEXT NOT NULL, capture_id TEXT UNIQUE, seller_merchant_id TEXT NOT NULL, currency TEXT NOT NULL, amount TEXT NOT NULL CHECK(CAST(amount AS REAL) > 0), status TEXT NOT NULL DEFAULT 'creating' CHECK(status IN ('creating', 'created', 'capturing', 'completed', 'failed')), created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(business_id, paypal_order_id), UNIQUE(business_id, create_request_id), UNIQUE(business_id, capture_request_id), FOREIGN KEY(business_id, invoice_id) REFERENCES invoices(business_id, id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS paypal_webhook_events (event_id TEXT PRIMARY KEY, event_type TEXT NOT NULL, received_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, processed_at DATETIME);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS products_business_created_idx ON catalog_products(business_id, created_at);
CREATE INDEX IF NOT EXISTS clients_business_created_idx ON clients(business_id, created_at);
CREATE INDEX IF NOT EXISTS invoices_business_created_idx ON invoices(business_id, created_at);
`
