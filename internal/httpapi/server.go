package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/matheusseabra/go-sqlite-api/internal/hot"
	"github.com/matheusseabra/go-sqlite-api/internal/persist"
)

type Server struct {
	hot    *hot.State
	writes *persist.Batcher
	static fs.FS
}

func New(h *hot.State, writes *persist.Batcher, static fs.FS) *Server {
	return &Server{hot: h, writes: writes, static: static}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.hot.Config()) })
	mux.HandleFunc("GET /v1/accounts/{id}", s.balance)
	mux.HandleFunc("POST /v1/accounts/{id}/charges", s.charge)
	mux.Handle("GET /", http.FileServerFS(s.static))
	return s.recover(s.headers(s.limit(mux)))
}
func (s *Server) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hot.Allow(clientKey(r.RemoteAddr), 1_000, time.Now()) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"account_id": r.PathValue("id"), "balance": s.hot.Balance(r.PathValue("id"))})
}
func (s *Server) charge(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     string `json:"id"`
		Amount int64  `json:"amount"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || !validID(input.ID) {
		http.Error(w, "id and amount are required", http.StatusBadRequest)
		return
	}
	accountID := r.PathValue("id")
	if err := s.hot.TryDeduct(accountID, input.Amount); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.writes.Enqueue(ctx, persist.Event{ID: input.ID, AccountID: accountID, Amount: input.Amount, Message: "charge " + strings.TrimSpace(input.ID)}); err != nil {
		s.hot.Credit(accountID, input.Amount)
		http.Error(w, "persistence unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func clientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
