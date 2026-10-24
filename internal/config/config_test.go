package config

import (
	"path/filepath"
	"testing"
)

func TestLoadUsesDevelopmentDefaults(t *testing.T) {
	clearDatabaseEnvironment(t)
	clearPayPalEnvironment(t)
	t.Setenv("PORT", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "18080" || cfg.Environment != "development" || cfg.SQLitePath != filepath.Join("data", "wisppay.db") {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadRejectsAddressInPort(t *testing.T) {
	clearPayPalEnvironment(t)
	t.Setenv("PORT", "127.0.0.1:8080")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid PORT to be rejected")
	}
}

func TestLoadRejectsOutOfRangePort(t *testing.T) {
	clearPayPalEnvironment(t)
	t.Setenv("PORT", "65536")
	if _, err := Load(); err == nil {
		t.Fatal("expected out-of-range PORT to be rejected")
	}
}

func TestProductionUsesLocalSQLiteAndIgnoresRemoteDatabaseEnvironment(t *testing.T) {
	clearDatabaseEnvironment(t)
	clearPayPalEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "mysql://remote.example/wisppay")
	setDatabasePartsEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SQLitePath != filepath.Join("data", "wisppay.db") {
		t.Fatalf("remote database settings affected SQLite path: %q", cfg.SQLitePath)
	}
}

func clearDatabaseEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"DATABASE_URL", "DATABASE_HOST", "DATABASE_PORT", "DATABASE_NAME",
		"DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_CA_CERT", "SSL_CERT",
	} {
		t.Setenv(name, "")
	}
}

func clearPayPalEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET", "PAYPAL_BASE_URL", "PAYPAL_PARTNER_MERCHANT_ID", "PAYPAL_PARTNER_ATTRIBUTION_ID", "PAYPAL_WEBHOOK_ID"} {
		t.Setenv(name, "")
	}
}

func TestPayPalSandboxCanBeEnabledWithCompleteCredentials(t *testing.T) {
	clearDatabaseEnvironment(t)
	clearPayPalEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("PAYPAL_CLIENT_ID", "client-id")
	t.Setenv("PAYPAL_CLIENT_SECRET", "client-secret")
	t.Setenv("PAYPAL_BASE_URL", "https://api-m.sandbox.paypal.com")
	t.Setenv("PAYPAL_PARTNER_MERCHANT_ID", "partner-merchant")
	t.Setenv("PAYPAL_PARTNER_ATTRIBUTION_ID", "partner-attribution")
	t.Setenv("PAYPAL_WEBHOOK_ID", "webhook-id")
	t.Setenv("PUBLIC_BASE_URL", "https://wisppay.example")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PayPalEnabled || cfg.PayPalBaseURL != "https://api-m.sandbox.paypal.com" {
		t.Fatalf("sandbox PayPal config = %+v", cfg)
	}
}

func TestPayPalRemainsDisabledWithoutPartnerConfiguration(t *testing.T) {
	clearDatabaseEnvironment(t)
	clearPayPalEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("PAYPAL_CLIENT_ID", "client-id")
	t.Setenv("PAYPAL_CLIENT_SECRET", "client-secret")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PayPalEnabled {
		t.Fatal("PayPal enabled without the connected-seller partner settings")
	}
}

func TestLoadUsesConfiguredSQLitePath(t *testing.T) {
	clearPayPalEnvironment(t)
	t.Setenv("APP_ENV", "development")
	clearDatabaseEnvironment(t)
	t.Setenv("SQLITE_PATH", "tmp/demo.db")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SQLitePath != "tmp/demo.db" {
		t.Fatalf("SQLite path = %q, want tmp/demo.db", cfg.SQLitePath)
	}
}

func TestLegacyDatabaseVariablesAreIgnored(t *testing.T) {
	clearDatabaseEnvironment(t)
	clearPayPalEnvironment(t)
	t.Setenv("APP_ENV", "development")
	setDatabasePartsEnvironment(t)
	t.Setenv("SSL_CERT", "REQUIRED")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SQLitePath != filepath.Join("data", "wisppay.db") {
		t.Fatalf("legacy database settings affected SQLite path: %q", cfg.SQLitePath)
	}
}

func setDatabasePartsEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_HOST", "mysql.example.com")
	t.Setenv("DATABASE_PORT", "24718")
	t.Setenv("DATABASE_NAME", "billing")
	t.Setenv("DATABASE_USER", "app-user")
	t.Setenv("DATABASE_PASSWORD", "not-a-real-secret")
}
