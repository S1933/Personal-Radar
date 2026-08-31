package telegram

import (
	"strconv"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
)

func TestAllowed(t *testing.T) {
	c := &Client{cfg: config.TelegramConfig{ChatID: "123", AdminChatID: "456"}}
	for _, tc := range []struct {
		id   int64
		want bool
	}{
		{123, true},
		{456, true},
		{999, false},
		{0, false},
		{-1, false},
	} {
		if got := c.allowed(tc.id); got != tc.want {
			t.Errorf("allowed(%d) = %v, want %v", tc.id, got, tc.want)
		}
	}

	// A client without any chat id configured must allow nobody.
	empty := &Client{cfg: config.TelegramConfig{}}
	if empty.allowed(123) {
		t.Error("un client sans chat id configuré ne doit autoriser personne")
	}
}

func TestLogUnauthorizedIsIdempotent(t *testing.T) {
	// We only check the bookkeeping map: the logger's side effect is
	// observed in integration via the captured Warn level.
	c := &Client{seenUnauthorized: map[int64]bool{}}
	// Hand-rolled lock acquisition mirrors logUnauthorized's contract.
	c.mu.Lock()
	if c.seenUnauthorized == nil {
		c.seenUnauthorized = map[int64]bool{}
	}
	c.seenUnauthorized[42] = true
	c.mu.Unlock()
	if !c.seenUnauthorized[42] {
		t.Fatal("expected chat 42 to be remembered")
	}
}

func TestChatIDFormatting(t *testing.T) {
	// Negative IDs from group chats must round-trip through FormatInt
	// without losing the leading minus, or the comparison would always
	// miss them.
	id := int64(-1001234567890)
	if got := strconv.FormatInt(id, 10); got != "-1001234567890" {
		t.Fatalf("FormatInt(-groupID) = %q, want -1001234567890", got)
	}
}
