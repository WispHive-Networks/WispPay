package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

type DB struct {
	*sql.DB
}

func Open(dsn, caCertPath string) (*DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is empty")
	}
	var db *sql.DB
	var err error
	if caCertPath == "" {
		db, err = sql.Open("mysql", dsn)
	} else {
		certificate, readErr := os.ReadFile(caCertPath)
		if readErr != nil {
			return nil, fmt.Errorf("read DATABASE_CA_CERT")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificate) {
			return nil, fmt.Errorf("DATABASE_CA_CERT contains no valid certificates")
		}
		config, parseErr := mysqldriver.ParseDSN(dsn)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid DATABASE_URL")
		}
		config.TLS = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		connector, connectorErr := mysqldriver.NewConnector(config)
		if connectorErr != nil {
			return nil, fmt.Errorf("configure MySQL TLS")
		}
		db = sql.OpenDB(connector)
	}
	if err != nil {
		return nil, fmt.Errorf("open MySQL pool: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(4 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	return &DB{DB: db}, nil
}

func (db *DB) Ready(ctx context.Context) error {
	if db == nil || db.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	return db.PingContext(ctx)
}

func ConnectionErrorCategory(err error) string {
	message := strings.ToLower(err.Error())
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return fmt.Sprintf("mysql server rejected the connection (code %d)", mysqlErr.Number)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "database host could not be resolved"
	}
	if strings.Contains(message, "no such host") || strings.Contains(message, "name resolution") {
		return "database host could not be resolved"
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return "database TLS certificate authority is not trusted"
	}
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return "database TLS certificate does not match the host"
	}
	if strings.Contains(message, "unknown authority") {
		return "database TLS certificate authority is not trusted"
	}
	if strings.Contains(message, "certificate is valid for") {
		return "database TLS certificate does not match the host"
	}
	var tlsHeaderError tls.RecordHeaderError
	if errors.As(err, &tlsHeaderError) {
		return "database TLS handshake failed"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "database host refused the connection"
	}
	if strings.Contains(message, "connection refused") {
		return "database host refused the connection"
	}
	if strings.Contains(message, "access denied") || strings.Contains(message, "authentication failed") {
		return "database server rejected the supplied credentials"
	}
	if strings.Contains(message, "tls") || strings.Contains(message, "ssl") {
		return "database TLS handshake failed"
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return "database connection timed out"
	}
	if strings.Contains(message, "i/o timeout") || strings.Contains(message, "deadline exceeded") {
		return "database connection timed out"
	}
	return "connection failed; check database availability, credentials, and TLS settings"
}
