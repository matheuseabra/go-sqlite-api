package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
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
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(s.hot.Config()) })
	mux.HandleFunc("GET /v1/accounts/{id}", s.balance)
	mux.HandleFunc("POST /v1/accounts/{id}/charges", s.charge)
	mux.Handle("GET /", http.FileServerFS(s.static))
	return s.limit(mux)
}
func (s *Server) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hot.Hit(r.RemoteAddr) > 1000 {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"account_id": r.PathValue("id"), "balance": s.hot.Balance(r.PathValue("id"))})
}
func (s *Server) charge(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     string `json:"id"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil || input.ID == "" {
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
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}
