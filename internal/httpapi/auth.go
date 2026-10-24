package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/auth"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

const (
	sessionCookieName = "wisppay_session"
	csrfCookieName    = "wisppay_csrf"
	sessionLifetime   = 12 * time.Hour
)

type identityStore interface {
	CreateBusinessUser(context.Context, string, string, string) error
	FindUserByEmail(context.Context, string) (mysqlstore.IdentityUser, error)
	CreateSession(context.Context, string, string, string, time.Time) error
	FindSession(context.Context, string) (mysqlstore.SessionIdentity, error)
	DeleteSession(context.Context, string) error
}

type authHandler struct{ store identityStore }

func registerAuthRoutes(mux *http.ServeMux, store identityStore) {
	handler := authHandler{store: store}
	mux.HandleFunc("GET /api/auth/csrf", handler.csrf)
	mux.HandleFunc("POST /api/auth/register", handler.register)
	mux.HandleFunc("POST /api/auth/login", handler.login)
	mux.HandleFunc("GET /api/auth/me", handler.me)
	mux.HandleFunc("POST /api/auth/logout", handler.logout)
}

func (h authHandler) csrf(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(csrfCookieName)
	if err == nil && len(cookie.Value) >= 40 {
		writeJSON(w, http.StatusOK, map[string]string{"csrfToken": cookie.Value})
		return
	}
	token, err := randomToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to initialize request security"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: token, Path: "/", HttpOnly: false, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLifetime.Seconds())})
	writeJSON(w, http.StatusOK, map[string]string{"csrfToken": token})
}

func (h authHandler) register(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(w, r, "") {
		return
	}
	var input struct {
		BusinessName string `json:"businessName"`
		Email        string `json:"email"`
		Password     string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	input.BusinessName = strings.TrimSpace(input.BusinessName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	parsedEmail, err := mail.ParseAddress(input.Email)
	if err != nil || parsedEmail.Address != input.Email || len(input.Email) > 254 || len(input.BusinessName) < 2 || len(input.BusinessName) > 120 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid business name and email are required"})
		return
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be between 12 and 128 bytes"})
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.store.CreateBusinessUser(ctx, input.BusinessName, input.Email, passwordHash); err != nil {
		if errors.Is(err, mysqlstore.ErrEmailExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "account could not be created with those details"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account could not be created"})
		return
	}
	user, err := h.store.FindUserByEmail(ctx, input.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "account created but sign-in could not be started"})
		return
	}
	h.createSession(w, r, ctx, user)
}

func (h authHandler) login(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(w, r, "") {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity service unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	user, err := h.store.FindUserByEmail(ctx, strings.ToLower(strings.TrimSpace(input.Email)))
	if err != nil || !auth.VerifyPassword(user.PasswordHash, input.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "email or password is incorrect"})
		return
	}
	h.createSession(w, r, ctx, user)
}

func (h authHandler) me(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": session.ID, "businessId": session.BusinessID,
		"businessName": session.BusinessName, "email": session.Email,
	})
}

func (h authHandler) logout(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok || !h.validCSRF(w, r, session.CSRFHash) {
		return
	}
	cookie, _ := r.Cookie(sessionCookieName)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.store.DeleteSession(ctx, tokenHash(cookie.Value)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session could not be closed"})
		return
	}
	clearCookie(w, r, sessionCookieName, true)
	clearCookie(w, r, csrfCookieName, false)
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandler) createSession(w http.ResponseWriter, r *http.Request, ctx context.Context, user mysqlstore.IdentityUser) {
	csrfCookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request security token is missing"})
		return
	}
	token, err := randomToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to start session"})
		return
	}
	if err := h.store.CreateSession(ctx, tokenHash(token), tokenHash(csrfCookie.Value), user.ID, time.Now().UTC().Add(sessionLifetime)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to start session"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: int(sessionLifetime.Seconds())})
	writeJSON(w, http.StatusCreated, map[string]any{"id": user.ID, "businessId": user.BusinessID, "businessName": user.BusinessName, "email": user.Email})
}

func (h authHandler) currentSession(w http.ResponseWriter, r *http.Request) (mysqlstore.SessionIdentity, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || h.store == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return mysqlstore.SessionIdentity{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	session, err := h.store.FindSession(ctx, tokenHash(cookie.Value))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return mysqlstore.SessionIdentity{}, false
	}
	return session, true
}

func (h authHandler) validCSRF(w http.ResponseWriter, r *http.Request, expectedHash string) bool {
	cookie, cookieErr := r.Cookie(csrfCookieName)
	header := r.Header.Get("X-CSRF-Token")
	if cookieErr != nil || len(cookie.Value) < 40 || len(header) != len(cookie.Value) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request security check failed"})
		return false
	}
	if expectedHash != "" && subtle.ConstantTimeCompare([]byte(expectedHash), []byte(tokenHash(cookie.Value))) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request security check failed"})
		return false
	}
	return true
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	return true
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(os.Getenv("APP_ENV"), "production")
}

func clearCookie(w http.ResponseWriter, r *http.Request, name string, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: httpOnly, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}
