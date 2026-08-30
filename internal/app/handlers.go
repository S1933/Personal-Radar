package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/S1933/personal-radar/internal/personalization"
	"github.com/S1933/personal-radar/internal/telegram"
)

// TelegramHandlers builds the command map served by the telegram listener.
func (a *App) TelegramHandlers() map[string]telegram.Handler {
	cmdHandler := &commandHandler{app: a}
	out := map[string]telegram.Handler{}
	for _, name := range []string{"start", "today", "deepdive", "save", "ignore", "more", "less", "sources", "status", "reaction", "ask", "newchat"} {
		out[name] = cmdHandler
	}
	return out
}

type commandHandler struct {
	app *App
}

func (h *commandHandler) Handle(ctx context.Context, cmd telegram.Command) (string, error) {
	a := h.app
	switch cmd.Action {
	case "start":
		return "☀️ Personal Radar actif.\n\n" +
			"/today — briefing du jour\n" +
			"/save N — archiver dans Obsidian\n" +
			"/ignore N — moins de ça\n" +
			"/more N — plus de ça\n" +
			"/less N — moins de ça\n" +
			"/sources — état des collecteurs\n" +
			"/status — stats du jour\n" +
			"Réactions: 👍 👎 🔥 📌 (avec le numéro de l'item)", nil

	case "today":
		return a.Briefer.Generate(ctx)

	case "save":
		if cmd.ItemID == 0 {
			return "", fmt.Errorf("usage: /save <id>")
		}
		if err := a.SaveToObsidian(ctx, cmd.ItemID); err != nil {
			return "", err
		}
		personalization.Apply(ctx, a.Prefs, cmd.ItemID, "save")
		return fmt.Sprintf("📌 Item %d archivé dans Obsidian.", cmd.ItemID), nil

	case "ignore":
		if cmd.ItemID == 0 {
			return "", fmt.Errorf("usage: /ignore <id>")
		}
		personalization.Apply(ctx, a.Prefs, cmd.ItemID, "ignore")
		return fmt.Sprintf("🚫 Item %d ignoré.", cmd.ItemID), nil

	case "more":
		if cmd.ItemID == 0 {
			return "", fmt.Errorf("usage: /more <id>")
		}
		personalization.Apply(ctx, a.Prefs, cmd.ItemID, "more_like_this")
		return "🔥 Noté: plus de ce type de contenu.", nil

	case "less":
		if cmd.ItemID == 0 {
			return "", fmt.Errorf("usage: /less <id>")
		}
		personalization.Apply(ctx, a.Prefs, cmd.ItemID, "less_like_this")
		return "📉 Noté: moins de ce type de contenu.", nil

	case "reaction":
		switch cmd.Emoji {
		case "👍":
			return h.Handle(ctx, telegram.Command{Action: "more", ItemID: cmd.ItemID})
		case "👎":
			return h.Handle(ctx, telegram.Command{Action: "less", ItemID: cmd.ItemID})
		case "🔥":
			return h.Handle(ctx, telegram.Command{Action: "more", ItemID: cmd.ItemID})
		case "📌":
			return h.Handle(ctx, telegram.Command{Action: "save", ItemID: cmd.ItemID})
		}
		return "", nil

	case "sources":
		var b strings.Builder
		b.WriteString("📡 Sources:\n")
		b.WriteString(fmt.Sprintf("RSS: %d feeds (%s)\n", len(a.Cfg.RSS.Feeds), enabled(a.Cfg.RSS.Enabled)))
		b.WriteString(fmt.Sprintf("Reddit: %d subs (%s)\n", len(a.Cfg.Reddit.Subreddits), enabled(a.Cfg.Reddit.Enabled)))
		b.WriteString(fmt.Sprintf("GitHub: %d repos, %d orgs (%s)\n", len(a.Cfg.GitHub.Repositories), len(a.Cfg.GitHub.Organizations), enabled(a.Cfg.GitHub.Enabled)))
		return b.String(), nil

	case "status":
		counts, err := a.Store.FeedbackCounts(ctx, 7)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString("📊 7 derniers jours:\n")
		for action, n := range counts {
			b.WriteString(fmt.Sprintf("%s: %d\n", action, n))
		}
		return b.String(), nil

	case "deepdive":
		if cmd.ItemID == 0 {
			return "", fmt.Errorf("usage: /deepdive <id>")
		}
		return a.DeepDiveItem(ctx, int64(cmd.ItemID))

	case "ask":
		// Forward the prompt to the local Hermes bridge (radar_bridge.py on the
		// host). The bridge calls `hermes chat -q` with --continue so messages
		// share the same conversation thread across calls.
		text := strings.TrimSpace(strings.TrimPrefix(cmd.Text, "/ask"))
		if text == "" {
			return "", fmt.Errorf("usage: /ask <question>")
		}
		return askHermes(ctx, text)

	case "newchat":
		// Start a fresh Hermes session (no --continue). Next /ask starts clean.
		text := strings.TrimSpace(strings.TrimPrefix(cmd.Text, "/newchat"))
		return askHermesRaw(ctx, text, true)
	}
	return "", nil
}

// askHermes forwards the prompt to the bridge using --continue (resume
// the most recent session so messages share context).
func askHermes(ctx context.Context, prompt string) (string, error) {
	return askHermesRaw(ctx, prompt, false)
}

// askHermesRaw POSTs {prompt, fresh} to HERMES_BRIDGE_URL and returns the
// assistant reply. fresh=true → bridge starts a new session instead of
// resuming the previous one.
func askHermesRaw(ctx context.Context, prompt string, fresh bool) (string, error) {
	url := os.Getenv("HERMES_BRIDGE_URL")
	if url == "" {
		url = "http://172.19.0.1:8765/ask"
	}
	body, err := json.Marshal(map[string]any{"prompt": prompt, "fresh": fresh})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("HERMES_BRIDGE_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge unreachable: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bridge HTTP %d: %s", res.StatusCode, truncate(string(raw), 200))
	}
	var parsed struct {
		Reply string `json:"reply"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("bridge bad json: %w", err)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("%s", parsed.Error)
	}
	if parsed.Reply == "" {
		return "", fmt.Errorf("bridge returned empty reply")
	}
	return parsed.Reply, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func enabled(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// SaveToObsidian writes the item as a markdown note in the vault.
func (a *App) SaveToObsidian(ctx context.Context, itemID int64) error {
	if !a.Cfg.Obsidian.Enabled || a.Cfg.Obsidian.VaultPath == "" {
		return fmt.Errorf("obsidian not configured")
	}
	it, err := a.Store.ItemByID(ctx, itemID)
	if err != nil {
		return err
	}
	dir := a.Cfg.Obsidian.VaultPath + "/Daily Radar/" + nowDateString() + "/"
	if err := mkdirAll(dir); err != nil && !os.IsExist(err) {
		return err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("source: " + it.Source + "\n")
	b.WriteString("url: " + it.URL + "\n")
	b.WriteString("saved: " + nowDateString() + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + it.Title + "\n\n")
	if it.Content != "" {
		b.WriteString(it.Content + "\n\n")
	}
	b.WriteString("[Lien](" + it.URL + ")\n")
	return writeFile(dir+fmt.Sprintf("item-%d.md", itemID), b.String())
}

var _ = strconv.Itoa
