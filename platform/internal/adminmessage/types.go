// Package adminmessage owns durable administrator broadcast drafts and delivery results.
package adminmessage

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type InformalName func(context.Context, map[string]any) (string, error)

type Service struct {
	DB           *pgxpool.Pool
	BotID        int64
	InformalName InformalName
}

// Destination accepts a numeric private/group ID or a public Telegram chat username.
type Destination struct {
	Chat   string `json:"chat"`
	Thread int64  `json:"thread,omitempty"`
}

type Content struct {
	Text        string `json:"text,omitempty"`
	ParseMode   string `json:"parse_mode,omitempty"`
	FromChat    int64  `json:"from_chat,omitempty"`
	FromMessage int64  `json:"from_message,omitempty"`
}

type Request struct {
	Destinations []Destination `json:"destinations"`
	Content      Content       `json:"content"`
}

type Message struct {
	ID      int64   `json:"id"`
	State   string  `json:"state"`
	Request Request `json:"request"`
}

type Delivery struct {
	ID                int64       `json:"id"`
	MessageID         int64       `json:"message_id"`
	Destination       Destination `json:"destination"`
	Content           Content     `json:"content"`
	State             string      `json:"state"`
	Attempt           int64       `json:"attempt"`
	TelegramMessageID int64       `json:"telegram_message_id"`
	Failure           string      `json:"failure"`
}

var usernamePattern = regexp.MustCompile(`^@[A-Za-z][A-Za-z0-9_]{4,31}$`)

const (
	maxTextUnits    = 4096
	maxKeyBytes     = 200
	maxFailureBytes = 1000
)

func problem(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }

func invalid() error { return problem(http.StatusBadRequest, "admin_message_invalid") }

func normalize(request Request) (Request, error) {
	if len(request.Destinations) == 0 {
		return Request{}, invalid()
	}
	if err := validateContent(request.Content); err != nil {
		return Request{}, err
	}
	result := Request{Content: request.Content, Destinations: make([]Destination, 0, len(request.Destinations))}
	seen := make(map[Destination]bool)
	for _, destination := range request.Destinations {
		if destination.Thread < 0 {
			return Request{}, invalid()
		}
		if usernamePattern.MatchString(destination.Chat) {
			destination.Chat = strings.ToLower(destination.Chat)
		} else {
			id, err := strconv.ParseInt(destination.Chat, 10, 64)
			if err != nil || id == 0 {
				return Request{}, invalid()
			}
			destination.Chat = strconv.FormatInt(id, 10)
		}
		if !seen[destination] {
			result.Destinations = append(result.Destinations, destination)
			seen[destination] = true
		}
	}
	return result, nil
}

func validateContent(content Content) error {
	if content.FromMessage != 0 || content.FromChat != 0 {
		if content.FromMessage <= 0 || content.FromChat == 0 || content.Text != "" || content.ParseMode != "" {
			return invalid()
		}
	} else if strings.TrimSpace(content.Text) == "" || len(utf16.Encode([]rune(content.Text))) > maxTextUnits {
		return invalid()
	}
	switch content.ParseMode {
	case "", parseHTML, "Markdown":
	default:
		return invalid()
	}
	return nil
}

func rejected(code string) error { return problem(http.StatusBadRequest, code) }
