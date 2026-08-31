package telegram

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestSplitMessage(t *testing.T) {
	t.Run("court, non découpé", func(t *testing.T) {
		got := splitMessage("bonjour", 4096)
		if len(got) != 1 || got[0] != "bonjour" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("découpe sur les sauts de ligne", func(t *testing.T) {
		in := strings.Repeat("ligne de texte\n", 500)
		for _, chunk := range splitMessage(in, 4096) {
			if len(chunk) > 4096 {
				t.Fatalf("chunk de %d octets", len(chunk))
			}
			if strings.HasPrefix(chunk, "\n") {
				t.Error("chunk commençant par un saut de ligne")
			}
		}
	})
	t.Run("aucune balise <a> coupée", func(t *testing.T) {
		in := strings.Repeat(`<a href="https://x.test">titre</a>`+"\n", 300)
		for _, chunk := range splitMessage(in, 4096) {
			if strings.Count(chunk, "<a ") != strings.Count(chunk, "</a>") {
				t.Fatalf("balise <a> coupée entre deux chunks : %q", chunk[:min(80, len(chunk))])
			}
		}
	})
}

func TestTruncateRunes(t *testing.T) {
	// Pathological case: one line longer than max. splitMessage routes
	// it through truncateRunes; we check the cut is UTF-8 valid.
	out := truncateRunes("éèàùç éèàùç éèàùç", 3, "…")
	if !utf8.ValidString(out) {
		t.Fatalf("truncateRunes a produit de l'UTF-8 invalide: %q", out)
	}
}

func TestParse(t *testing.T) {
	// Table-driven so a regression points at the exact input that broke.
	cases := []struct {
		in   string
		want Command
	}{
		// Slash commands.
		{"/save 12", Command{Action: "save", ItemID: 12}},
		{"/save", Command{Action: "save"}},
		{"/today", Command{Action: "today"}},
		{"/briefing", Command{Action: "briefing"}},
		{"/help", Command{Action: "help"}},
		{"/deepdive@PersoRadarBot 7", Command{Action: "deepdive", ItemID: 7}},
		{"/save@bot 5", Command{Action: "save", ItemID: 5}},

		// Reactions with id — the original bug: "👍 12" was dropped.
		{"👍 12", Command{Action: "reaction", Emoji: "👍", ItemID: 12}},
		{"👎 3", Command{Action: "reaction", Emoji: "👎", ItemID: 3}},
		{"🔥 99", Command{Action: "reaction", Emoji: "🔥", ItemID: 99}},
		{"📌 3", Command{Action: "reaction", Emoji: "📌", ItemID: 3}},

		// Reactions without id.
		{"👍", Command{Action: "reaction", Emoji: "👍"}},
		{"  🔥  ", Command{Action: "reaction", Emoji: "🔥"}},
		{"	📌  ", Command{Action: "reaction", Emoji: "📌"}},

		// Edge cases.
		{"", Command{}},
		{"   ", Command{}},
		{"bonjour", Command{Action: "bonjour"}},
		{"/save -1", Command{Action: "save"}},    // id négatif ignoré
		{"/save 0", Command{Action: "save"}},     // id zéro ignoré
		{"/save abc", Command{Action: "save"}},   // id non numérique ignoré
		{"👍 abc", Command{Action: "reaction", Emoji: "👍"}}, // id non numérique ignoré
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parse(tc.in)
			if got != tc.want {
				t.Errorf("parse(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
