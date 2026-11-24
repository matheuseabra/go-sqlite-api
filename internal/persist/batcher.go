package persist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matheusseabra/go-sqlite-api/internal/store"
)

const QueueCapacity = 8192

type Event struct {
	ID, AccountID, Message string
	Amount                 int64
}
type Batcher struct {
	store     *store.Store
	queue     chan Event
	accepting atomic.Bool
	submitMu  sync.RWMutex
	done      chan struct{}
	errMu     sync.Mutex
	err       error
}

func New(s *store.Store) *Batcher {
	b := &Batcher{store: s, queue: make(chan Event, QueueCapacity), done: make(chan struct{})}
	b.accepting.Store(true)
	go b.run()
	return b
}
func (b *Batcher) Enqueue(ctx context.Context, e Event) error {
	b.submitMu.RLock()
	defer b.submitMu.RUnlock()
	if !b.accepting.Load() {
		return errors.New("persistence queue is shutting down")
	}
	select {
	case b.queue <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Batcher) Shutdown(ctx context.Context) error {
	b.submitMu.Lock()
	if b.accepting.Swap(false) {
		close(b.queue)
	}
	b.submitMu.Unlock()
	select {
	case <-b.done:
		return b.failure()
	case <-ctx.Done():
		return fmt.Errorf("drain persistence queue: %w", ctx.Err())
	}
}
func (b *Batcher) failure() error { b.errMu.Lock(); defer b.errMu.Unlock(); return b.err }
func (b *Batcher) fail(err error) {
	b.errMu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.errMu.Unlock()
}
func (b *Batcher) run() {
	defer close(b.done)
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]Event, 0, 64)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := b.persist(batch); err != nil {
			b.fail(err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case e, ok := <-b.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, e)
			if len(batch) == cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
func (b *Batcher) persist(events []Event) error {
	return b.store.Write(context.Background(), func(tx *sql.Tx) error {
		for _, e := range events {
			if e.Amount > 0 {
				if _, err := tx.Exec("INSERT INTO charges(id, account_id, amount) VALUES(?, ?, ?)", e.ID, e.AccountID, e.Amount); err != nil {
					return err
				}
				result, err := tx.Exec("UPDATE accounts SET balance = balance - ?, updated_at = CURRENT_TIMESTAMP WHERE account_id = ? AND balance >= ?", e.Amount, e.AccountID, e.Amount)
				if err != nil {
					return err
				}
				changed, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if changed != 1 {
					return fmt.Errorf("account %q has insufficient durable balance", e.AccountID)
				}
			}
			if e.Message != "" {
				if _, err := tx.Exec("INSERT INTO request_logs(message) VALUES(?)", e.Message); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
