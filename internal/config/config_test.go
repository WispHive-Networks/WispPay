package config

import "testing"

func TestLoadUsesDevelopmentDefaults(t *testing.T) {
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
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "user:pass@tcp(db.example:3306)/wisppay?tls=false")
	if _, err := Load(); err == nil {
		t.Fatal("expected production MySQL without verified TLS to be rejected")
	}
}

func TestProductionRequiresDatabase(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected production without DATABASE_URL to be rejected")
	}
}
