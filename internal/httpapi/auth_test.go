package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WispHive-Networks/WispPay/internal/auth"
	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

type memoryIdentityStore struct {
	users    map[string]mysqlstore.IdentityUser
	sessions map[string]mysqlstore.SessionIdentity
}

func (s *memoryIdentityStore) CreateBusinessUser(_ context.Context, name, email, passwordHash string) error {
	if s.users == nil {
		s.users = make(map[string]mysqlstore.IdentityUser)
	}
	s.users[email] = mysqlstore.IdentityUser{ID: "user-id", BusinessID: "business-id", BusinessName: name, Email: email, PasswordHash: passwordHash}
	return nil
}

func (s *memoryIdentityStore) FindUserByEmail(_ context.Context, email string) (mysqlstore.IdentityUser, error) {
	user, ok := s.users[email]
	if !ok {
		return mysqlstore.IdentityUser{}, context.DeadlineExceeded
	}
	return user, nil
}

func (s *memoryIdentityStore) CreateSession(_ context.Context, tokenHash, csrfHash, userID string, expiresAt time.Time) error {
	if s.sessions == nil {
		s.sessions = make(map[string]mysqlstore.SessionIdentity)
	}
	user := s.users["owner@example.com"]
	s.sessions[tokenHash] = mysqlstore.SessionIdentity{IdentityUser: user, CSRFHash: csrfHash}
	return nil
}

func (s *memoryIdentityStore) FindSession(_ context.Context, tokenHash string) (mysqlstore.SessionIdentity, error) {
	session, ok := s.sessions[tokenHash]
	if !ok {
		return mysqlstore.SessionIdentity{}, context.DeadlineExceeded
	}
	return session, nil
}

func (s *memoryIdentityStore) DeleteSession(_ context.Context, tokenHash string) error {
	delete(s.sessions, tokenHash)
	return nil
}

func TestRegistrationCreatesSessionAndLogoutInvalidatesIt(t *testing.T) {
	store := &memoryIdentityStore{}
	mux := http.NewServeMux()
	registerAuthRoutes(mux, store)

	csrfResponse := httptest.NewRecorder()
	mux.ServeHTTP(csrfResponse, httptest.NewRequest(http.MethodGet, "/api/auth/csrf", nil))
	csrfCookie := csrfResponse.Result().Cookies()[0]
	var csrfBody struct {
		Token string `json:"csrfToken"`
	}
	if err := json.Unmarshal(csrfResponse.Body.Bytes(), &csrfBody); err != nil {
		t.Fatal(err)
	}
	if csrfBody.Token != csrfCookie.Value {
		t.Fatal("CSRF token response did not match its cookie")
	}

	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"businessName":"Example Studio","email":"owner@example.com","password":"a-secure-test-password"}`))
	register.AddCookie(csrfCookie)
	register.Header.Set("Content-Type", "application/json")
	register.Header.Set("X-CSRF-Token", csrfBody.Token)
	registerResponse := httptest.NewRecorder()
	mux.ServeHTTP(registerResponse, register)
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d: %s", registerResponse.Code, http.StatusCreated, registerResponse.Body.String())
	}
	storedUser := store.users["owner@example.com"]
	if storedUser.PasswordHash == "a-secure-test-password" || !auth.VerifyPassword(storedUser.PasswordHash, "a-secure-test-password") {
		t.Fatal("registration did not persist a verifiable password hash")
	}
	sessionCookie := registerResponse.Result().Cookies()[0]
	if sessionCookie.Name != sessionCookieName || !sessionCookie.HttpOnly {
		t.Fatal("session cookie is missing or not HttpOnly")
	}

	me := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	me.AddCookie(sessionCookie)
	meResponse := httptest.NewRecorder()
	mux.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusOK || !strings.Contains(meResponse.Body.String(), "Example Studio") {
		t.Fatalf("authenticated /me response = %d %s", meResponse.Code, meResponse.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logout.Header.Set("X-CSRF-Token", csrfBody.Token)
	logoutResponse := httptest.NewRecorder()
	mux.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutResponse.Code, http.StatusNoContent)
	}
	meResponse = httptest.NewRecorder()
	mux.ServeHTTP(meResponse, me)
	if meResponse.Code != http.StatusUnauthorized {
		t.Fatalf("/me after logout status = %d, want %d", meResponse.Code, http.StatusUnauthorized)
	}
}
