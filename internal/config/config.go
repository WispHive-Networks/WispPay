package config

import (
	"fmt"
	"os"
	"strings"
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
		cfg.Port = "8080"
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if strings.ContainsAny(cfg.Port, ":/\\ \t\r\n") {
		return Config{}, fmt.Errorf("PORT must contain only a port number")
	}
	if cfg.Environment == "production" && cfg.DatabaseDSN != "" && !strings.Contains(cfg.DatabaseDSN, "tls=") {
		return Config{}, fmt.Errorf("production DATABASE_URL must enable MySQL TLS")
	}
	return cfg, nil
}
