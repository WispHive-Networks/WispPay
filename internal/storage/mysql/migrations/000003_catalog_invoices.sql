CREATE TABLE IF NOT EXISTS catalog_categories (
    id CHAR(32) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    name VARCHAR(80) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY categories_business_name_unique (business_id, name),
    UNIQUE KEY categories_business_id_unique (business_id, id),
    CONSTRAINT categories_business_fk FOREIGN KEY (business_id) REFERENCES businesses (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS catalog_products (
    id CHAR(32) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    category_id CHAR(32) NULL,
    name VARCHAR(120) NOT NULL,
    description VARCHAR(2000) NOT NULL DEFAULT '',
    price DECIMAL(13,2) NOT NULL,
    currency CHAR(3) NOT NULL,
    available BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY products_business_created_idx (business_id, created_at),
    KEY products_business_available_idx (business_id, available),
    UNIQUE KEY products_business_id_unique (business_id, id),
    CONSTRAINT products_business_fk FOREIGN KEY (business_id) REFERENCES businesses (id) ON DELETE CASCADE,
    CONSTRAINT products_category_business_fk FOREIGN KEY (business_id, category_id) REFERENCES catalog_categories (business_id, id) ON DELETE RESTRICT,
    CONSTRAINT products_price_nonnegative CHECK (price >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS clients (
    id CHAR(32) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    display_name VARCHAR(120) NOT NULL,
    email VARCHAR(254) NULL,
    phone VARCHAR(32) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    KEY clients_business_created_idx (business_id, created_at),
    UNIQUE KEY clients_business_id_unique (business_id, id),
    CONSTRAINT clients_business_fk FOREIGN KEY (business_id) REFERENCES businesses (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS invoices (
    id CHAR(32) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    client_id CHAR(32) NOT NULL,
    invoice_reference VARCHAR(32) NOT NULL,
    currency CHAR(3) NOT NULL,
    subtotal DECIMAL(13,2) NOT NULL,
    total DECIMAL(13,2) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY invoices_business_reference_unique (business_id, invoice_reference),
    UNIQUE KEY invoices_business_id_unique (business_id, id),
    KEY invoices_business_created_idx (business_id, created_at),
    CONSTRAINT invoices_business_fk FOREIGN KEY (business_id) REFERENCES businesses (id) ON DELETE CASCADE,
    CONSTRAINT invoices_client_business_fk FOREIGN KEY (business_id, client_id) REFERENCES clients (business_id, id) ON DELETE RESTRICT,
    CONSTRAINT invoices_subtotal_nonnegative CHECK (subtotal >= 0),
    CONSTRAINT invoices_total_nonnegative CHECK (total >= 0),
    CONSTRAINT invoices_status_valid CHECK (status IN ('draft', 'open', 'paid', 'cancelled', 'expired'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS invoice_items (
    id CHAR(32) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    invoice_id CHAR(32) NOT NULL,
    product_id CHAR(32) NULL,
    product_name_snapshot VARCHAR(120) NOT NULL,
    description_snapshot VARCHAR(2000) NOT NULL,
    quantity INT UNSIGNED NOT NULL,
    unit_price DECIMAL(13,2) NOT NULL,
    line_total DECIMAL(13,2) NOT NULL,
    KEY invoice_items_invoice_idx (business_id, invoice_id),
    CONSTRAINT invoice_items_invoice_business_fk FOREIGN KEY (business_id, invoice_id) REFERENCES invoices (business_id, id) ON DELETE CASCADE,
    CONSTRAINT invoice_items_product_business_fk FOREIGN KEY (business_id, product_id) REFERENCES catalog_products (business_id, id) ON DELETE RESTRICT,
    CONSTRAINT invoice_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT invoice_items_amounts_nonnegative CHECK (unit_price >= 0 AND line_total >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS public_invoice_tokens (
    token_hash CHAR(64) NOT NULL PRIMARY KEY,
    business_id CHAR(32) NOT NULL,
    invoice_id CHAR(32) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY public_tokens_invoice_idx (business_id, invoice_id),
    KEY public_tokens_expiry_idx (expires_at),
    CONSTRAINT public_tokens_invoice_business_fk FOREIGN KEY (business_id, invoice_id) REFERENCES invoices (business_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;