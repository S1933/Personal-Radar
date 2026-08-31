package briefing

import (
	"strings"
	"testing"
)

// TestEscapeHTML guards against the original bug: a title containing "_"
// or "*" would, under Markdown v1, produce unbalanced entities and a 400
// from the Telegram API. The fix is HTML, where only &, <, > and quotes
// need escaping — and html.EscapeString handles all four.
func TestEscapeHTML(t *testing.T) {
	// The exact title from the report that broke the briefing.
	title := `net/http: fix _leak_ in *Transport* & <body>`
	out := escapeHTML(title)
	// <body> must be escaped to &lt;body&gt;
	if strings.Contains(out, "<body>") {
		t.Errorf("<body> non échappé: %q", out)
	}
	// & must be escaped to &amp;
	if strings.Contains(out, " & ") {
		t.Errorf("& non échappé: %q", out)
	}
	// Underscore/asterisk must stay literal — they are not HTML
	// metacharacters and were the actual reason the whole message broke.
	if !strings.Contains(out, "_leak_") {
		t.Errorf("tirets bas devraient rester intacts : %q", out)
	}
	if !strings.Contains(out, "*Transport*") {
		t.Errorf("astérisques devraient rester intacts : %q", out)
	}
}

func TestEscapeHTMLQuote(t *testing.T) {
	// Quotes matter inside href attributes — Telegram's HTML parser
	// requires balanced quotes around the URL.
	out := escapeHTML(`href="https://x.test/?q=a&b"`)
	if strings.Contains(out, `"`) {
		t.Errorf("quote non échappée: %q", out)
	}
}
