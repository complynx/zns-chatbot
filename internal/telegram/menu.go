package telegram

import (
	"context"
	"regexp"
	"unicode/utf8"
)

// BotCommand is a command shown in Telegram's chat menu.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

var commandName = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

const (
	maxMenuCommands       = 100
	maxCommandDescription = 256
)

type chatCommandScope struct {
	Type   string `json:"type"`
	ChatID int64  `json:"chat_id"`
}

type commandsMenuButton struct {
	Type string `json:"type"`
}

// SetChatCommands replaces only one chat's command list. Reapplying the same list
// after a lost response is safe; it does not send a message or run a command.
func (c *Client) SetChatCommands(ctx context.Context, chatID int64, language string, commands []BotCommand) error {
	if chatID <= 0 || (language != "" && language != "ru") || len(commands) == 0 || len(commands) > maxMenuCommands {
		return &Error{Outcome: NotStarted}
	}
	for _, command := range commands {
		size := utf8.RuneCountInString(command.Description)
		if !commandName.MatchString(command.Command) || size < 1 || size > maxCommandDescription {
			return &Error{Outcome: NotStarted}
		}
	}
	var result bool
	err := c.call(ctx, "setMyCommands", struct {
		Commands []BotCommand     `json:"commands"`
		Scope    chatCommandScope `json:"scope"`
		Language string           `json:"language_code,omitempty"`
	}{Commands: commands, Scope: chatCommandScope{Type: "chat", ChatID: chatID}, Language: language}, &result, true)
	if err == nil && !result {
		return &Error{Outcome: Rejected}
	}
	return err
}

// SetChatMenuCommands selects the command menu for this chat, not a global or
// WebApp menu. MiniApp launch buttons remain separate messages.
func (c *Client) SetChatMenuCommands(ctx context.Context, chatID int64) error {
	if chatID <= 0 {
		return &Error{Outcome: NotStarted}
	}
	var result bool
	err := c.call(ctx, "setChatMenuButton", struct {
		ChatID int64              `json:"chat_id"`
		Menu   commandsMenuButton `json:"menu_button"`
	}{ChatID: chatID, Menu: commandsMenuButton{Type: "commands"}}, &result, true)
	if err == nil && !result {
		return &Error{Outcome: Rejected}
	}
	return err
}
