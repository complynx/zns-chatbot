package telegram

import (
	"context"
	"errors"
)

// BotCommand is one command in Telegram's localized command picker.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type MenuButton struct {
	Type string `json:"type"`
}

// SetDefaultCommandsMenu selects the command picker for chats without an override.
func (c Client) SetDefaultCommandsMenu(ctx context.Context) error {
	var applied bool
	err := c.Call(ctx, "setChatMenuButton", struct {
		MenuButton MenuButton `json:"menu_button"`
	}{MenuButton: MenuButton{Type: "commands"}}, &applied)
	if err != nil {
		return err
	}
	if !applied {
		return errors.New("telegram menu button was not applied")
	}
	return nil
}

// SetMyCommands replaces the default-scope commands for one Telegram language.
// Empty language selects the fallback command list, not a separate English override.
func (c Client) SetMyCommands(ctx context.Context, language string, commands []BotCommand) error {
	var applied bool
	err := c.Call(ctx, "setMyCommands", struct {
		Commands     []BotCommand `json:"commands"`
		LanguageCode string       `json:"language_code,omitempty"`
	}{Commands: commands, LanguageCode: language}, &applied)
	if err != nil {
		return err
	}
	if !applied {
		return errors.New("telegram commands were not applied")
	}
	return nil
}
