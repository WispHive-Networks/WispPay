package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	Environment    string
	DatabaseDSN    string
	DatabaseCACert string
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
		Port:           strings.TrimSpace(os.Getenv("PORT")),
		Environment:    strings.TrimSpace(os.Getenv("APP_ENV")),
		DatabaseDSN:    strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DatabaseCACert: strings.TrimSpace(os.Getenv("DATABASE_CA_CERT")),
	}
	if cfg.DatabaseCACert == "" {
		cfg.DatabaseCACert = strings.TrimSpace(os.Getenv("SSL_CERT"))
	}
	if cfg.DatabaseCACert == "" {
		defaultCACert := filepath.Join("cert", "ca.pem")
		if info, err := os.Stat(defaultCACert); err == nil && info.Mode().IsRegular() {
			cfg.DatabaseCACert = defaultCACert
		} else if err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("check default database CA certificate")
		}
	}
	databaseHost := strings.TrimSpace(os.Getenv("DATABASE_HOST"))
	if databaseHost != "" {
		dsn, err := databaseDSNFromParts(databaseHost, os.Getenv("DATABASE_PORT"), os.Getenv("DATABASE_NAME"), os.Getenv("DATABASE_USER"), os.Getenv("DATABASE_PASSWORD"))
		if err != nil {
			return Config{}, err
		}
		cfg.DatabaseDSN = dsn
	} else if cfg.DatabaseDSN != "" {
		dsn, err := NormalizeDatabaseURL(cfg.DatabaseDSN)
		if err != nil {
			return Config{}, err
		}
		cfg.DatabaseDSN = dsn
	}
	if cfg.Port == "" {
		cfg.Port = "18080"
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.Environment == "production" && cfg.DatabaseDSN == "" {
		return Config{}, fmt.Errorf("production DATABASE_URL is required")
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be a valid TCP port")
	}
	if cfg.Environment == "production" && cfg.DatabaseDSN != "" {
		dsn, err := mysqldriver.ParseDSN(cfg.DatabaseDSN)
		if err != nil {
			return Config{}, fmt.Errorf("invalid DATABASE_URL")
		}
		if dsn.TLSConfig == "" || dsn.TLSConfig == "false" || dsn.TLSConfig == "skip-verify" || dsn.TLSConfig == "preferred" {
			return Config{}, fmt.Errorf("production DATABASE_URL must enable verified MySQL TLS")
		}
	}
	return cfg, nil
}

func databaseDSNFromParts(host, port, name, user, password string) (string, error) {
	port = strings.TrimSpace(port)
	name = strings.TrimSpace(name)
	user = strings.TrimSpace(user)
	if strings.ContainsAny(host, "/?#@ \t\r\n") || name == "" || user == "" || password == "" {
		return "", fmt.Errorf("DATABASE_HOST, DATABASE_PORT, DATABASE_NAME, DATABASE_USER, and DATABASE_PASSWORD must be configured")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("DATABASE_PORT must be a valid TCP port")
	}
	config := mysqldriver.NewConfig()
	config.User = user
	config.Passwd = password
	config.Net = "tcp"
	config.Addr = net.JoinHostPort(host, port)
	config.DBName = name
	config.ParseTime = true
	config.TLSConfig = "true"
	return config.FormatDSN(), nil
}

func NormalizeDatabaseURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid DATABASE_URL")
	}
	if parsed.Scheme == "" {
		return value, nil
	}
	if parsed.Scheme != "mysql" || parsed.User == nil || parsed.Hostname() == "" || strings.Trim(parsed.Path, "/") == "" {
		return "", fmt.Errorf("DATABASE_URL must be a MySQL DSN or mysql:// URL")
	}
	if len(parsed.Query()) > 1 || len(parsed.Query()) == 1 && parsed.Query().Get("ssl-mode") == "" {
		return "", fmt.Errorf("DATABASE_URL contains unsupported MySQL URL options")
	}
	sslMode := strings.ToUpper(parsed.Query().Get("ssl-mode"))
	if sslMode != "REQUIRED" && sslMode != "VERIFY_CA" && sslMode != "VERIFY_IDENTITY" {
		return "", fmt.Errorf("MySQL URL ssl-mode must require verified TLS")
	}
	port := parsed.Port()
	if port == "" {
		port = "3306"
	}
	config := mysqldriver.NewConfig()
	config.User = parsed.User.Username()
	config.Passwd, _ = parsed.User.Password()
	config.Net = "tcp"
	config.Addr = net.JoinHostPort(parsed.Hostname(), port)
	config.DBName = strings.Trim(parsed.Path, "/")
	config.ParseTime = true
	config.TLSConfig = "true"
	return config.FormatDSN(), nil
}
