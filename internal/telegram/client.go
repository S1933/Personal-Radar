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
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/logging"
)

// Client talks to the Telegram Bot API: long-polling updates + sendMessage.
type Client struct {
	cfg      config.TelegramConfig
	log      *logging.Logger
	client   *http.Client
	handlers map[string]Handler
}

// Handler processes a command or reaction; returns the reply text.
type Handler interface {
	Handle(ctx context.Context, cmd Command) (string, error)
}

// Command is a parsed user interaction.
type Command struct {
	Action string // start, today, deepdive, save, ignore, more, less, sources, status, reaction, ask, newchat
	ItemID int64
	Emoji  string
	Text   string // full message text (used by handlers that need the raw prompt)
}

func NewClient(cfg config.TelegramConfig, log *logging.Logger) (*Client, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
	}
	cfg.AdminChatID = firstNonEmpty(os.Getenv("TELEGRAM_CHAT_ID"), cfg.AdminChatID)
	cfg.ChatID = firstNonEmpty(cfg.ChatID, cfg.AdminChatID)
	return &Client{cfg: cfg, log: log, client: &http.Client{Timeout: 65 * time.Second}}, nil
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

// Send posts a markdown message to the configured chat.
func (c *Client) Send(ctx context.Context, text string) error {
	if c.cfg.ChatID == "" {
		return fmt.Errorf("no chat id configured")
	}
	_, err := c.api("sendMessage", map[string]any{
		"chat_id":    c.cfg.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	return err
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
			cmd := parse(u.Message.Text)
			reply := c.dispatch(ctx, cmd)
			if reply != "" && c.cfg.ChatID != "" {
				if _, err := c.api("sendMessage", map[string]any{
					"chat_id": c.cfg.ChatID, "text": reply, "parse_mode": "Markdown",
				}); err != nil {
					c.log.Warn("reply", "error", err)
				}
			}
		}
	}
}

func parse(text string) Command {
	t := strings.TrimSpace(text)
	switch t {
	case "👍":
		return Command{Action: "reaction", Emoji: "👍"}
	case "👎":
		return Command{Action: "reaction", Emoji: "👎"}
	case "🔥":
		return Command{Action: "reaction", Emoji: "🔥"}
	case "📌":
		return Command{Action: "reaction", Emoji: "📌"}
	}
	fields := strings.Fields(t)
	if len(fields) == 0 {
		return Command{}
	}
	cmd := strings.TrimPrefix(fields[0], "/")
	c := Command{Action: cmd, Text: t}
	if len(fields) > 1 {
		if id, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
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
