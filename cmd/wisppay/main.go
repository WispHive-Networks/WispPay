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
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
	"github.com/WispHive-Networks/WispPay/web"
)

func main() {
	migrate := flag.Bool("migrate", false, "apply pending database migrations and exit")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	var db *mysqlstore.DB
	if cfg.DatabaseDSN != "" {
		db, err = mysqlstore.Open(cfg.DatabaseDSN)
		if err != nil {
			logger.Error("database connection failed; check DATABASE_URL and database availability")
			os.Exit(1)
		}
		defer db.Close()
	}
	if *migrate {
		if db == nil {
			logger.Error("DATABASE_URL is required for migrations")
			os.Exit(1)
		}
		if err := mysqlstore.Migrate(context.Background(), db.DB); err != nil {
			logger.Error("database migration failed", "error", err)
			os.Exit(1)
		}
		logger.Info("database migrations complete")
		return
	}

	server := &http.Server{
		Addr:              "0.0.0.0:" + cfg.Port,
		Handler:           httpapi.New(db, web.Files, logger),
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
