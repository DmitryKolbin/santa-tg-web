package mini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type Bot struct {
	token, appURL string
	client        HTTPClient
	queue         chan notification
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type notification struct {
	user int64
	text string
}

func NewBot(token, appURL string) *Bot {
	return NewBotWithClient(token, appURL, &http.Client{Timeout: 40 * time.Second})
}

func NewBotWithClient(token, appURL string, client HTTPClient) *Bot {
	return &Bot{token: token, appURL: appURL, client: client, queue: make(chan notification, 512)}
}

func (b *Bot) call(ctx context.Context, method string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+b.token+"/"+method, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram request: invalid configuration")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: transport unavailable", method)
	}
	defer res.Body.Close()
	var envelope struct {
		OK        bool            `json:"ok"`
		Result    json.RawMessage `json:"result"`
		ErrorCode int             `json:"error_code"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("telegram %s: invalid response", method)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram %s: code %d", method, envelope.ErrorCode)
	}
	if output != nil {
		return json.Unmarshal(envelope.Result, output)
	}
	return nil
}
func (b *Bot) Configure(ctx context.Context) (string, error) {
	var user struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, "getMe", struct{}{}, &user); err != nil {
		return "", err
	}
	if err := b.call(ctx, "setChatMenuButton", map[string]any{"menu_button": map[string]any{"type": "web_app", "text": "Открыть Санту", "web_app": map[string]string{"url": b.appURL}}}, nil); err != nil {
		return "", err
	}
	err := b.call(ctx, "setMyCommands", map[string]any{"commands": []map[string]string{{"command": "start", "description": "Открыть Secret Santa"}, {"command": "help", "description": "Как пользоваться"}}}, nil)
	return user.Username, err
}

// ponytail: notifications are best effort and queued in memory; use a durable
// outbox if retries across restarts become a requirement.
func (b *Bot) Notify(user int64, text string) {
	select {
	case b.queue <- notification{user, text}:
	default:
		slog.Warn("notification queue full")
	}
}
func (b *Bot) send(ctx context.Context, n notification) error {
	return b.call(ctx, "sendMessage", map[string]any{"chat_id": n.user, "text": n.text, "reply_markup": map[string]any{"inline_keyboard": [][]any{{map[string]any{"text": "Открыть Secret Santa", "web_app": map[string]string{"url": b.appURL}}}}}}, nil)
}
func (b *Bot) Run(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case n := <-b.queue:
				if err := b.send(ctx, n); err != nil && ctx.Err() == nil {
					slog.Warn("notification failed", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}
	}()
	offset := int64(0)
	for ctx.Err() == nil {
		var updates []struct {
			ID      int64 `json:"update_id"`
			Message *struct {
				Text string `json:"text"`
				Chat struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"chat"`
			} `json:"message"`
		}
		err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram polling failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, update := range updates {
			offset = update.ID + 1
			if update.Message != nil && update.Message.Chat.Type == "private" {
				b.Notify(update.Message.Chat.ID, "Анкета, игры и анонимная переписка теперь в приложении. Откройте Secret Santa кнопкой ниже.")
			}
		}
	}
}
