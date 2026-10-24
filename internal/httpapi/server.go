package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	mysqlstore "github.com/WispHive-Networks/WispPay/internal/storage/mysql"
)

type readiness interface {
	Ready(context.Context) error
}

func New(db readiness, assets http.FileSystem, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	identity, _ := db.(identityStore)
	registerAuthRoutes(mux, identity)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if db == nil || db.Ready(r.Context()) != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("/", spaHandler(assets))
	return requestLogger(securityHeaders(mux), logger)
}

func spaHandler(files http.FileSystem) http.Handler {
	fileServer := http.FileServer(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if file, err := files.Open(r.URL.Path); err == nil {
			_ = file.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := files.Open("/index.html")
		if err != nil {
			http.Error(w, "application assets unavailable", http.StatusInternalServerError)
			return
		}
		defer index.Close()
		info, err := index.Stat()
		if err != nil {
			http.Error(w, "application assets unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", info.ModTime(), index.(io.ReadSeeker))
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if len(requestID) > 64 || strings.ContainsAny(requestID, "\r\n") {
			requestID = ""
		}
		if requestID == "" {
			var id [16]byte
			if _, err := rand.Read(id[:]); err == nil {
				requestID = hex.EncodeToString(id[:])
			} else {
				requestID = "unavailable"
			}
		}
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		next.ServeHTTP(w, r)
		path := r.URL.Path
		if strings.HasPrefix(path, "/i/") {
			path = "/i/:token"
		}
		logger.Info("http request", "request_id", requestID, "method", r.Method, "path", path, "duration_ms", time.Since(started).Milliseconds())
	})
}

var _ readiness = (*mysqlstore.DB)(nil)
