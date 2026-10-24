CREATE TABLE IF NOT EXISTS paypal_seller_accounts (
    business_id CHAR(32) NOT NULL PRIMARY KEY,
    tracking_id CHAR(32) NOT NULL,
    onboarding_state_hash CHAR(64) NULL,
    merchant_id VARCHAR(64) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    payments_receivable BOOLEAN NOT NULL DEFAULT FALSE,
    primary_email_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
    payment_permission BOOLEAN NOT NULL DEFAULT FALSE,
    connected_at TIMESTAMP NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY paypal_seller_tracking_unique (tracking_id),
    UNIQUE KEY paypal_seller_merchant_unique (merchant_id),
    CONSTRAINT paypal_seller_business_fk FOREIGN KEY (business_id) REFERENCES businesses (id) ON DELETE CASCADE,
    CONSTRAINT paypal_seller_status_valid CHECK (status IN ('pending', 'active', 'revoked'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS invoice_paypal_payments (
    id CHAR(32) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    invoice_id CHAR(32) NOT NULL,
    paypal_order_id VARCHAR(36) NULL,
    approval_url VARCHAR(2048) NULL,
    create_request_id CHAR(32) NOT NULL,
    capture_request_id CHAR(32) NOT NULL,
    capture_id VARCHAR(64) NULL,
    seller_merchant_id VARCHAR(64) NOT NULL,
    currency CHAR(3) NOT NULL,
    amount DECIMAL(13,2) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'creating',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY invoice_paypal_order_unique (business_id, paypal_order_id),
    UNIQUE KEY invoice_paypal_create_request_unique (business_id, create_request_id),
    UNIQUE KEY invoice_paypal_capture_request_unique (business_id, capture_request_id),
    UNIQUE KEY invoice_paypal_capture_unique (capture_id),
    KEY invoice_paypal_invoice_status_idx (business_id, invoice_id, status),
    CONSTRAINT invoice_paypal_invoice_fk FOREIGN KEY (business_id, invoice_id) REFERENCES invoices (business_id, id) ON DELETE CASCADE,
    CONSTRAINT invoice_paypal_amount_nonnegative CHECK (amount > 0),
    CONSTRAINT invoice_paypal_status_valid CHECK (status IN ('creating', 'created', 'capturing', 'completed', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS paypal_webhook_events (
    event_id VARCHAR(128) NOT NULL PRIMARY KEY,
    event_type VARCHAR(128) NOT NULL,
    received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMP NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;