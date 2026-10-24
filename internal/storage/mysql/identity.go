package mysql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

var ErrEmailExists = errors.New("email already registered")

type IdentityUser struct {
	ID           string
	BusinessID   string
	BusinessName string
	Email        string
	PasswordHash string
}

type SessionIdentity struct {
	IdentityUser
	CSRFHash string
}

func (db *DB) CreateBusinessUser(ctx context.Context, businessName, email, passwordHash string) error {
	businessID, err := randomID()
	if err != nil {
		return err
	}
	userID, err := randomID()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin registration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO businesses (id, name) VALUES (?, ?)", businessID, businessName); err != nil {
		return fmt.Errorf("create business: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO users (id, business_id, email, password_hash) VALUES (?, ?, ?, ?)", userID, businessID, email, passwordHash); err != nil {
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrEmailExists
		}
		return fmt.Errorf("create user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit registration: %w", err)
	}
	return nil
}

func (db *DB) FindUserByEmail(ctx context.Context, email string) (IdentityUser, error) {
	var user IdentityUser
	err := db.QueryRowContext(ctx, `SELECT u.id, u.business_id, b.name, u.email, u.password_hash
		FROM users u JOIN businesses b ON b.id = u.business_id WHERE u.email = ?`, email).
		Scan(&user.ID, &user.BusinessID, &user.BusinessName, &user.Email, &user.PasswordHash)
	if err != nil {
		return IdentityUser{}, err
	}
	return user, nil
}

func (db *DB) CreateSession(ctx context.Context, tokenHash, csrfHash, userID string, expiresAt time.Time) error {
	_, err := db.ExecContext(ctx, "INSERT INTO sessions (token_hash, user_id, csrf_hash, expires_at) VALUES (?, ?, ?, ?)", tokenHash, userID, csrfHash, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (db *DB) FindSession(ctx context.Context, tokenHash string) (SessionIdentity, error) {
	var session SessionIdentity
	err := db.QueryRowContext(ctx, `SELECT u.id, u.business_id, b.name, u.email, s.csrf_hash
		FROM sessions s JOIN users u ON u.id = s.user_id JOIN businesses b ON b.id = u.business_id
		WHERE s.token_hash = ? AND s.expires_at > UTC_TIMESTAMP()`, tokenHash).
		Scan(&session.ID, &session.BusinessID, &session.BusinessName, &session.Email, &session.CSRFHash)
	if err != nil {
		return SessionIdentity{}, err
	}
	return session, nil
}

func (db *DB) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate record ID: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
