// Package hot keeps latency-sensitive state out of SQLite.
package hot

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type account struct {
	mu      sync.Mutex
	balance int64
}

type State struct {
	config   atomic.Value // map[string]string; replace snapshots, never mutate them.
	routes   atomic.Value // map[string]string; replace snapshots, never mutate them.
	accounts sync.Map     // map[string]*account
	rate     sync.Map     // map[string]*atomic.Uint64
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
func (s *State) Hit(key string) uint64 {
	v, _ := s.rate.LoadOrStore(key, &atomic.Uint64{})
	return v.(*atomic.Uint64).Add(1)
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
