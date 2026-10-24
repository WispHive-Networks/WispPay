package sqlite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

func (db *DB) CreateBusinessUser(ctx context.Context, businessName, email, passwordHash string) error {
	businessID, err := newID()
	if err != nil {
		return err
	}
	userID, err := newID()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO businesses (id, name) VALUES (?, ?)", businessID, businessName); err != nil {
		return fmt.Errorf("create business: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO users (id, business_id, email, password_hash) VALUES (?, ?, ?, ?)", userID, businessID, email, passwordHash); err != nil {
		if isUniqueConstraint(err) {
			return mysqlstore.ErrEmailExists
		}
		return fmt.Errorf("create user: %w", err)
	}
	return tx.Commit()
}

func (db *DB) FindUserByEmail(ctx context.Context, email string) (mysqlstore.IdentityUser, error) {
	var user mysqlstore.IdentityUser
	err := db.QueryRowContext(ctx, `SELECT u.id, u.business_id, b.name, u.email, u.password_hash
		FROM users u JOIN businesses b ON b.id = u.business_id WHERE u.email = ?`, email).
		Scan(&user.ID, &user.BusinessID, &user.BusinessName, &user.Email, &user.PasswordHash)
	return user, err
}

func (db *DB) CreateSession(ctx context.Context, tokenHash, csrfHash, userID string, expiresAt time.Time) error {
	_, err := db.ExecContext(ctx, "INSERT INTO sessions (token_hash, user_id, csrf_hash, expires_at) VALUES (?, ?, ?, ?)", tokenHash, userID, csrfHash, expiresAt.UTC())
	return err
}

func (db *DB) FindSession(ctx context.Context, tokenHash string) (mysqlstore.SessionIdentity, error) {
	var session mysqlstore.SessionIdentity
	err := db.QueryRowContext(ctx, `SELECT u.id, u.business_id, b.name, u.email, s.csrf_hash
		FROM sessions s JOIN users u ON u.id = s.user_id JOIN businesses b ON b.id = u.business_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, time.Now().UTC()).
		Scan(&session.ID, &session.BusinessID, &session.BusinessName, &session.Email, &session.CSRFHash)
	return session, err
}

func (db *DB) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate record ID: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
