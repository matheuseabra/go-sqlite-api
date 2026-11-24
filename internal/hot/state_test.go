package hot

import (
	"testing"
	"time"
)

func TestAllowUsesClientSpecificMinuteWindows(t *testing.T) {
	state := New()
	now := time.Unix(1_000, 0)
	if !state.Allow("one", 2, now) || !state.Allow("one", 2, now) || state.Allow("one", 2, now) {
		t.Fatal("expected a two-request limit")
	}
	if !state.Allow("two", 2, now) {
		t.Fatal("client keys must be isolated")
	}
	if !state.Allow("one", 2, now.Add(time.Minute)) {
		t.Fatal("expected a new minute window")
	}
}
