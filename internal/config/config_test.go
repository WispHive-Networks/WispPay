package config

import (
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestLoadUsesDevelopmentDefaults(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("PORT", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "18080" || cfg.Environment != "development" || cfg.DatabaseDSN != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadRejectsAddressInPort(t *testing.T) {
	t.Setenv("PORT", "127.0.0.1:8080")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid PORT to be rejected")
	}
}

func TestLoadRejectsOutOfRangePort(t *testing.T) {
	t.Setenv("PORT", "65536")
	if _, err := Load(); err == nil {
		t.Fatal("expected out-of-range PORT to be rejected")
	}
}

func TestProductionRequiresVerifiedDatabaseTLS(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "user:pass@tcp(db.example:3306)/wisppay?tls=false")
	if _, err := Load(); err == nil {
		t.Fatal("expected production MySQL without verified TLS to be rejected")
	}
}

func TestProductionRequiresDatabase(t *testing.T) {
	clearDatabaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected production without DATABASE_URL to be rejected")
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

func TestNormalizeDatabaseURLEnablesVerifiedTLS(t *testing.T) {
	dsn, err := NormalizeDatabaseURL("mysql://user:p%40ss@db.example:3307/billing?ssl-mode=REQUIRED")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.User != "user" || parsed.Passwd != "p@ss" || parsed.Addr != "db.example:3307" || parsed.DBName != "billing" || parsed.TLSConfig != "true" || !parsed.ParseTime {
		t.Fatalf("normalized DSN does not preserve required settings")
	}
}

func TestLoadBuildsVerifiedTLSDSNFromDatabaseParts(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_HOST", "mysql.example.com")
	t.Setenv("DATABASE_PORT", "24718")
	t.Setenv("DATABASE_NAME", "billing")
	t.Setenv("DATABASE_USER", "app-user")
	t.Setenv("DATABASE_PASSWORD", "not-a-real-secret")
	for _, name := range []string{"DATABASE_CA_CERT", "SSL_CERT"} {
		t.Setenv(name, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mysqldriver.ParseDSN(cfg.DatabaseDSN)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Addr != "mysql.example.com:24718" || parsed.DBName != "billing" || parsed.User != "app-user" || parsed.TLSConfig != "true" || !parsed.ParseTime {
		t.Fatal("split database configuration did not produce the expected verified-TLS DSN")
	}
}

func TestNormalizeDatabaseURLRejectsInsecureOrUnsupportedURLs(t *testing.T) {
	for _, value := range []string{
		"mysql://user:pass@db.example/billing?ssl-mode=DISABLED",
		"postgres://user:pass@db.example/billing",
		"mysql://user:pass@db.example/billing?ssl-mode=REQUIRED&other=value",
	} {
		if _, err := NormalizeDatabaseURL(value); err == nil {
			t.Fatalf("expected URL to be rejected: %q", value)
		}
	}
}
