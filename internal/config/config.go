package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                string
	Environment         string
	SQLitePath          string
	PayPalClientID      string
	PayPalClientSecret  string
	PayPalBaseURL       string
	PayPalPartnerID     string
	PayPalAttributionID string
	PayPalWebhookID     string
	PublicBaseURL       string
	PayPalEnabled       bool
}

func Load() (Config, error) {
	if strings.TrimSpace(os.Getenv("APP_ENV")) != "production" {
		if _, err := os.Stat(".env"); err == nil {
			if err := godotenv.Load(".env"); err != nil {
				return Config{}, fmt.Errorf("load .env: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("check .env: %w", err)
		}
	}
	cfg := Config{
		Port:                strings.TrimSpace(os.Getenv("PORT")),
		Environment:         strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))),
		SQLitePath:          strings.TrimSpace(os.Getenv("SQLITE_PATH")),
		PayPalClientID:      strings.TrimSpace(os.Getenv("PAYPAL_CLIENT_ID")),
		PayPalClientSecret:  strings.TrimSpace(os.Getenv("PAYPAL_CLIENT_SECRET")),
		PayPalBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("PAYPAL_BASE_URL")), "/"),
		PayPalPartnerID:     strings.TrimSpace(os.Getenv("PAYPAL_PARTNER_MERCHANT_ID")),
		PayPalAttributionID: strings.TrimSpace(os.Getenv("PAYPAL_PARTNER_ATTRIBUTION_ID")),
		PayPalWebhookID:     strings.TrimSpace(os.Getenv("PAYPAL_WEBHOOK_ID")),
		PublicBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")), "/"),
	}
	if cfg.Port == "" {
		cfg.Port = "18080"
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.PublicBaseURL == "" {
		cfg.PublicBaseURL = "http://localhost:" + cfg.Port
	}
	if cfg.SQLitePath == "" {
		cfg.SQLitePath = filepath.Join("data", "wisppay.db")
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be a valid TCP port")
	}
	if err := validatePayPalConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validatePayPalConfig(cfg *Config) error {
	if cfg.PayPalClientID == "" && cfg.PayPalClientSecret == "" {
		return nil
	}
	if cfg.PayPalClientID == "" || cfg.PayPalClientSecret == "" {
		return fmt.Errorf("PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET must be configured together")
	}
	if cfg.PayPalPartnerID == "" || cfg.PayPalAttributionID == "" {
		return nil
	}
	if cfg.PayPalBaseURL == "" {
		cfg.PayPalBaseURL = "https://api-m.sandbox.paypal.com"
	}
	parsed, err := url.Parse(cfg.PayPalBaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || parsed.Port() != "" {
		return fmt.Errorf("PAYPAL_BASE_URL must be an approved PayPal HTTPS API origin")
	}
	if parsed.Hostname() != "api-m.sandbox.paypal.com" && parsed.Hostname() != "api-m.paypal.com" {
		return fmt.Errorf("PAYPAL_BASE_URL must be an approved PayPal HTTPS API origin")
	}
	publicURL, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || publicURL.Hostname() == "" || publicURL.User != nil || publicURL.RawQuery != "" || publicURL.Fragment != "" || publicURL.Path != "" {
		return fmt.Errorf("PUBLIC_BASE_URL must be an origin without a path or query when PayPal is enabled")
	}
	if publicURL.Scheme != "https" && !(cfg.Environment != "production" && publicURL.Scheme == "http" && (publicURL.Hostname() == "localhost" || net.ParseIP(publicURL.Hostname()) != nil && net.ParseIP(publicURL.Hostname()).IsLoopback())) {
		return fmt.Errorf("PUBLIC_BASE_URL must use HTTPS, except for local development")
	}
	cfg.PayPalEnabled = true
	return nil
}
