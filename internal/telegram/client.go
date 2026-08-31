package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/logging"
	"github.com/S1933/personal-radar/internal/textutil"
)

// Client talks to the Telegram Bot API: long-polling updates + sendMessage.
type Client struct {
	cfg              config.TelegramConfig
	log              *logging.Logger
	client           *http.Client
	handlers         map[string]Handler
	mu               sync.Mutex
	seenUnauthorized map[int64]bool
}

// Handler processes a command or reaction; returns the reply text.
type Handler interface {
	Handle(ctx context.Context, cmd Command) (string, error)
}

// Command is a parsed user interaction.
type Command struct {
	Action string // start, today, deepdive, save, ignore, more, less, sources, status, reaction
	ItemID int64
	Emoji  string
}

func NewClient(cfg config.TelegramConfig, log *logging.Logger) (*Client, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
	}
	cfg.AdminChatID = firstNonEmpty(os.Getenv("TELEGRAM_CHAT_ID"), cfg.AdminChatID)
	cfg.ChatID = firstNonEmpty(cfg.ChatID, cfg.AdminChatID)
	// Without a chat id the bot would silently accept any sender that
	// finds @PersoRadarBot. Failing loudly at startup is better than a
	// chat that mysteriously never replies.
	if cfg.ChatID == "" && cfg.AdminChatID == "" {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID not set: refusing to run an unrestricted bot")
	}
	return &Client{cfg: cfg, log: log, client: &http.Client{Timeout: 65 * time.Second}}, nil
}

// allowed reports whether an incoming chat may drive the bot. Without this
// check, anyone who finds @PersoRadarBot could trigger LLM calls (/deepdive,
// /today) and disk writes (/save) at the owner's expense.
func (c *Client) allowed(chatID int64) bool {
	id := strconv.FormatInt(chatID, 10)
	return (c.cfg.ChatID != "" && id == c.cfg.ChatID) ||
		(c.cfg.AdminChatID != "" && id == c.cfg.AdminChatID)
}

// logUnauthorized records a Warn the first time a given unknown chat
// reaches us. Subsequent messages from the same chat are dropped silently:
// an unexpected sender is worth noticing, but a spam loop must not flood
// the logs.
func (c *Client) logUnauthorized(chatID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seenUnauthorized == nil {
		c.seenUnauthorized = map[int64]bool{}
	}
	if c.seenUnauthorized[chatID] {
		return
	}
	c.seenUnauthorized[chatID] = true
	c.log.Warn("ignoring message from unauthorized chat", "chat_id", chatID)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) api(method string, payload any) ([]byte, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	res, err := c.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	json.Unmarshal(b, &envelope)
	if !envelope.OK {
		return b, fmt.Errorf("telegram %s: %s", method, envelope.Description)
	}
	return b, nil
}

// Send posts an HTML message to the configured chat, splitting it across
// several messages when it exceeds Telegram's 4096-character limit.
//
// On a parse error (malformed entity in a source title we failed to
// escape), the chunk is retried as plain text: a briefing with lost
// formatting beats no briefing at all.
func (c *Client) Send(ctx context.Context, text string) error {
	if c.cfg.ChatID == "" {
		return fmt.Errorf("no chat id configured")
	}
	for _, chunk := range splitMessage(text, maxMessageLen) {
		if _, err := c.api("sendMessage", map[string]any{
			"chat_id":                  c.cfg.ChatID,
			"text":                     chunk,
			"parse_mode":               "HTML",
			"disable_web_page_preview": true,
		}); err != nil {
			c.log.Warn("send with HTML failed, retrying as plain text", "error", err)
			if _, err2 := c.api("sendMessage", map[string]any{
				"chat_id": c.cfg.ChatID,
				"text":    chunk,
			}); err2 != nil {
				return err2
			}
		}
	}
	return nil
}

// maxMessageLen is Telegram's hard limit on sendMessage text.
const maxMessageLen = 4096

// splitMessage cuts s into chunks of at most max bytes, always on a line
// boundary. Lines are never split mid-way: each line holds complete HTML
// tags, so breaking between lines can never produce an unbalanced entity.
func splitMessage(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	var out []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if len(line) > max {
			// Pathological single line (e.g. one giant URL): cut on
			// a rune boundary so the chunk is still valid UTF-8.
			line = textutil.Truncate(line, max/4, "…")
		}
		if buf.Len()+len(line)+1 > max {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
	}
	flush()
	return out
}

// RegisterHandler attaches a command handler.
func (c *Client) RegisterHandler(pattern string, h Handler) {
	if c.handlers == nil {
		c.handlers = map[string]Handler{}
	}
	c.handlers[pattern] = h
}

// Listen runs the long-polling loop until ctx is cancelled.
func (c *Client) Listen(ctx context.Context, handlers map[string]Handler) error {
	c.handlers = handlers
	offset := 0
	c.log.Info("telegram listener started")
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		payload := map[string]any{"timeout": 50, "allowed_updates": []string{"message"}}
		if offset > 0 {
			payload["offset"] = offset
		}
		b, err := c.api("getUpdates", payload)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Warn("getUpdates", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		var upd struct {
			Result []struct {
				UpdateID int64 `json:"update_id"`
				Message  *struct {
					Text string `json:"text"`
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
				} `json:"message"`
			} `json:"result"`
		}
		if err := json.Unmarshal(b, &upd); err != nil {
			c.log.Warn("decode updates", "error", err)
			continue
		}
		for _, u := range upd.Result {
			offset = int(u.UpdateID) + 1
			if u.Message == nil || strings.TrimSpace(u.Message.Text) == "" {
				continue
			}
			if !c.allowed(u.Message.Chat.ID) {
				c.logUnauthorized(u.Message.Chat.ID)
				continue
			}
			cmd := parse(u.Message.Text)
			reply := c.dispatch(ctx, cmd)
			if reply != "" && c.cfg.ChatID != "" {
				// HTML for the same reason as Send(): a source we
				// don't control is replying with content that may
				// contain "_" or "*" and break Markdown v1.
				if _, err := c.api("sendMessage", map[string]any{
					"chat_id":                  c.cfg.ChatID,
					"text":                     reply,
					"parse_mode":               "HTML",
					"disable_web_page_preview": true,
				}); err != nil {
					c.log.Warn("reply", "error", err)
				}
			}
		}
	}
}

// parse turns a raw Telegram message into a Command.
//
// Two shapes are supported, both may carry an item id:
//   "/save 12"   → {Action: "save", ItemID: 12}
//   "👍 12"      → {Action: "reaction", Emoji: "👍", ItemID: 12}
//
// The emoji is matched on the first field, not on the whole message: the
// previous version switched on the trimmed text, so any reaction carrying
// an id fell through and was silently dropped — the feedback loop was
// effectively dead for the whole life of the product.
func parse(text string) Command {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return Command{}
	}

	var c Command
	switch head := fields[0]; head {
	case "👍", "👎", "🔥", "📌":
		c.Action = "reaction"
		c.Emoji = head
	default:
		action := strings.TrimPrefix(head, "/")
		// Telegram appends "@BotName" to commands sent in groups.
		if i := strings.IndexByte(action, '@'); i > 0 {
			action = action[:i]
		}
		c.Action = action
	}

	if len(fields) > 1 {
		if id, err := strconv.ParseInt(fields[1], 10, 64); err == nil && id > 0 {
			c.ItemID = id
		}
	}
	return c
}

func (c *Client) dispatch(ctx context.Context, cmd Command) string {
	h, ok := c.handlers[cmd.Action]
	if !ok {
		if cmd.Action == "reaction" {
			if h, ok = c.handlers["reaction"]; !ok {
				return ""
			}
		} else {
			return ""
		}
	}
	out, err := h.Handle(ctx, cmd)
	if err != nil {
		c.log.Warn("handler", "action", cmd.Action, "error", err)
		return "⚠️ " + err.Error()
	}
	return out
}
