package textutil

import (
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"bonjour", 20, "bonjour"},
		{"bonjour", 3, "bon…"},
		{"éèàùç", 3, "éèà…"},                       // 2 bytes per rune
		{"café", 4, "café"},
		{"日本語テキスト", 3, "日本語…"},              // 3 bytes per rune
		{"", 5, ""},
		{"abc", 0, "…"},
		{"abc", -1, "…"},                          // n <= 0 returns suffix
	}
	for _, tc := range cases {
		got := Truncate(tc.in, tc.n, "…")
		if got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("Truncate(%q, %d) produced invalid UTF-8: %q", tc.in, tc.n, got)
		}
	}
}

func TestTruncateWords(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		// Short string, returned unchanged.
		{"court", 100, "court"},
		// Cut inside a word, no close space → just truncate.
		{"abcdefghij", 5, "abcde…"},
		// Cut inside a word, a space close enough → back off.
		{"hello world foo", 8, "hello…"},
		// Cut after a space, no need to back off.
		{"hello world", 5, "hello…"},
		// French accents at the cut.
		{"année académique 2026", 10, "année…"},
	}
	for _, tc := range cases {
		got := TruncateWords(tc.in, tc.n, "…")
		if got != tc.want {
			t.Errorf("TruncateWords(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TruncateWords(%q, %d) produced invalid UTF-8: %q", tc.in, tc.n, got)
		}
	}
}

func TestLastSpace(t *testing.T) {
	if got := lastSpace("hello world"); got != 5 {
		t.Errorf("lastSpace(\"hello world\") = %d, want 5", got)
	}
	if got := lastSpace("nospace"); got != -1 {
		t.Errorf("lastSpace(\"nospace\") = %d, want -1", got)
	}
	if got := lastSpace(""); got != -1 {
		t.Errorf("lastSpace(\"\") = %d, want -1", got)
	}
}

func TestCollapseWhitespace(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello   world", "hello world"},
		{"\n\thello\nworld  ", "hello world"},
		{"   ", ""},
		{"", ""},
		{"single", "single"},
	}
	for _, tc := range cases {
		if got := CollapseWhitespace(tc.in); got != tc.want {
			t.Errorf("CollapseWhitespace(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// FuzzTruncate: a valid UTF-8 input must always produce a valid UTF-8
// output. The whole point of this package is to never emit U+FFFD.
func FuzzTruncate(f *testing.F) {
	f.Add("Émeraude à Zürich 日本", 5)
	f.Fuzz(func(t *testing.T, s string, n int) {
		if n < 0 || n > 1000 {
			t.Skip()
		}
		if got := Truncate(s, n, "…"); utf8.ValidString(s) && !utf8.ValidString(got) {
			t.Fatalf("valid input produced invalid output: %q", got)
		}
	})
}
