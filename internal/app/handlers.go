package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/S1933/personal-radar/internal/personalization"
	"github.com/S1933/personal-radar/internal/telegram"
)

// TelegramHandlers builds the command map served by the telegram listener.
func (a *App) TelegramHandlers() map[string]telegram.Handler {
	cmdHandler := &commandHandler{app: a}
	out := map[string]telegram.Handler{}
	for _, name := range []string{"start", "today", "briefing", "help", "deepdive", "save", "ignore", "more", "less", "sources", "status", "reaction"} {
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
	case "start", "help":
		return "☀️ Personal Radar actif.\n\n" +
			"/today — briefing du jour\n" +
			"/save N — archiver dans Obsidian\n" +
			"/ignore N — moins de ça\n" +
			"/more N — plus de ça\n" +
			"/less N — moins de ça\n" +
			"/sources — état des collecteurs\n" +
			"/status — stats du jour\n" +
			"Réactions: 👍 👎 🔥 📌 (avec le numéro de l'item)", nil

	case "today", "briefing":
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
		if cmd.ItemID == 0 {
			return "Ajoute le numéro de l'item : par exemple <code>👍 12</code> pour liker l'item 12.", nil
		}
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
	}
	return "", nil
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
	dir := filepath.Join(a.Cfg.Obsidian.VaultPath, "Daily Radar", nowDateString())
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
	return writeFile(filepath.Join(dir, fmt.Sprintf("item-%d.md", itemID)), b.String())
}

// obsidianFilePath is the contract between SaveToObsidian and the
// filesystem: <VaultPath>/Daily Radar/<YYYY-MM-DD>/item-<id>.md.
// Extracted so the path logic is testable without a live database —
// the "Daily Radar" segment is the kind of string that gets fat-fingered.
func obsidianFilePath(vaultPath string, id int64, date string) string {
	return filepath.Join(vaultPath, "Daily Radar", date, fmt.Sprintf("item-%d.md", id))
}
