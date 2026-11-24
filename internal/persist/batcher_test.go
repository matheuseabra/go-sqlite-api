package persist_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/matheusseabra/go-sqlite-api/internal/persist"
	"github.com/matheusseabra/go-sqlite-api/internal/store"
)

func TestShutdownDrainsQueuedEvents(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.WriteDB.Exec("INSERT INTO accounts(account_id, balance) VALUES(?, ?)", "a_1", 10); err != nil {
		t.Fatal(err)
	}
	b := persist.New(db)
	if err := b.Enqueue(context.Background(), persist.Event{ID: "ch_1", AccountID: "a_1", Amount: 7, Message: "test"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	var count, balance int
	if err := db.ReadDB.QueryRow("SELECT COUNT(*) FROM charges").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.ReadDB.QueryRow("SELECT balance FROM accounts WHERE account_id = ?", "a_1").Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if count != 1 || balance != 3 {
		t.Fatalf("got count=%d balance=%d", count, balance)
	}
}
