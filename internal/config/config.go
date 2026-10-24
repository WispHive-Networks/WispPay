package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
)

type Config struct {
	Port        string
	Environment string
	DatabaseDSN string
}

func Load() (Config, error) {
	cfg := Config{
		Port:        strings.TrimSpace(os.Getenv("PORT")),
		Environment: strings.TrimSpace(os.Getenv("APP_ENV")),
		DatabaseDSN: strings.TrimSpace(os.Getenv("DATABASE_URL")),
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
