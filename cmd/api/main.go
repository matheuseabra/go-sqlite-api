package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/matheusseabra/go-sqlite-api/internal/hot"
	"github.com/matheusseabra/go-sqlite-api/internal/httpapi"
	"github.com/matheusseabra/go-sqlite-api/internal/persist"
	"github.com/matheusseabra/go-sqlite-api/internal/store"
)

//go:embed web/*
var web embed.FS

func main() {
	addr := env("API_ADDR", ":8080")
	dbPath := env("API_DB_PATH", "./data/api.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		log.Fatalf("create database directory: %v", err)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	writes := persist.New(db)
	hotState := hot.New()
	balances, err := db.LoadBalances(ctx)
	if err != nil {
		log.Fatalf("load account balances: %v", err)
	}
	for accountID, balance := range balances {
		hotState.SetBalance(accountID, balance)
	}
	static, err := fs.Sub(web, "web")
	if err != nil {
		log.Fatalf("load embedded assets: %v", err)
	}
	server := &http.Server{Addr: addr, Handler: httpapi.New(hotState, writes, static).Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		log.Printf("listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	if err := writes.Shutdown(shutdown); err != nil {
		log.Printf("persistence drain: %v", err)
	}
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
