package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/config"
	"github.com/WispHive-Networks/WispPay/internal/httpapi"
	"github.com/WispHive-Networks/WispPay/internal/paypal"
	sqlitestore "github.com/WispHive-Networks/WispPay/internal/storage/sqlite"
	"github.com/WispHive-Networks/WispPay/web"
)

func main() {
	checkDB := flag.Bool("check-db", false, "verify database connectivity and exit")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	db, err := sqlitestore.Open(cfg.SQLitePath)
	if err != nil {
		logger.Error("SQLite database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if *checkDB {
		logger.Info("database connectivity verified")
		return
	}
	var paypalIntegration httpapi.PayPalIntegration
	if cfg.PayPalEnabled {
		client, err := paypal.NewClient(cfg.PayPalClientID, cfg.PayPalClientSecret, cfg.PayPalBaseURL, nil)
		if err != nil {
			logger.Error("PayPal client initialization failed", "error", err)
			os.Exit(1)
		}
		paypalIntegration = httpapi.PayPalIntegration{
			Client: client,
			Settings: httpapi.PayPalSettings{
				Enabled:           true,
				PartnerMerchantID: cfg.PayPalPartnerID,
				AttributionID:     cfg.PayPalAttributionID,
				WebhookID:         cfg.PayPalWebhookID,
				PublicBaseURL:     cfg.PublicBaseURL,
			},
		}
	} else {
		logger.Info("PayPal Sandbox is disabled; configure client credentials, partner IDs, and public origin to enable it")
	}

	server := &http.Server{
		Addr:              "0.0.0.0:" + cfg.Port,
		Handler:           httpapi.New(db, web.Files, logger, paypalIntegration),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("server starting", "address", server.Addr, "environment", cfg.Environment)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
