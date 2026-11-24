// Package hot keeps latency-sensitive state out of SQLite.
package hot

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type account struct {
	mu      sync.Mutex
	balance int64
}

type rateCounter struct {
	window atomic.Int64
	hits   atomic.Uint64
}

type State struct {
	config   atomic.Value // map[string]string; replace snapshots, never mutate them.
	routes   atomic.Value // map[string]string; replace snapshots, never mutate them.
	accounts sync.Map     // map[string]*account
	rate     sync.Map     // map[string]*rateCounter
}

func New() *State {
	s := &State{}
	s.config.Store(map[string]string{"service": "go-sqlite-api"})
	s.routes.Store(map[string]string{"health": "/healthz"})
	return s
}

func (s *State) Config() map[string]string { return clone(s.config.Load().(map[string]string)) }
func (s *State) Route(name string) (string, bool) {
	v, ok := s.routes.Load().(map[string]string)[name]
	return v, ok
}

// Allow applies a fixed one-minute in-memory limit. It is intentionally local
// to one process; use an edge proxy for a distributed rate limit.
func (s *State) Allow(key string, limit uint64, now time.Time) bool {
	v, _ := s.rate.LoadOrStore(key, &rateCounter{})
	counter := v.(*rateCounter)
	window := now.Unix() / 60
	for {
		observed := counter.window.Load()
		if observed == window {
			return counter.hits.Add(1) <= limit
		}
		if counter.window.CompareAndSwap(observed, window) {
			counter.hits.Store(0)
		}
	}
}

func (s *State) SetBalance(id string, balance int64) {
	v, _ := s.accounts.LoadOrStore(id, &account{})
	a := v.(*account)
	a.mu.Lock()
	a.balance = balance
	a.mu.Unlock()
}
func (s *State) Balance(id string) int64 {
	v, ok := s.accounts.Load(id)
	if !ok {
		return 0
	}
	a := v.(*account)
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.balance
}
func (s *State) TryDeduct(id string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	v, _ := s.accounts.LoadOrStore(id, &account{})
	a := v.(*account)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.balance < amount {
		return fmt.Errorf("insufficient balance")
	}
	a.balance -= amount
	return nil
}
func (s *State) Credit(id string, amount int64) {
	v, _ := s.accounts.LoadOrStore(id, &account{})
	a := v.(*account)
	a.mu.Lock()
	a.balance += amount
	a.mu.Unlock()
}
func clone(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
