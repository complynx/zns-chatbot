package task

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf16"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const externalAgentServerName = "zinusya-mcp"

func externalAgentInstructionsView(
	capability loom.ExternalAgentCapability, profile loom.ExternalAgentProfile, ru bool,
) (string, *telegram.InlineKeyboardMarkup) {
	keyboard := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{
		Text:         structuredText(ru, "Back to settings", "Назад к настройкам"),
		CallbackData: fmt.Sprintf("%s%d:%s", externalAgentCallbackPrefix, profile.Version, externalAgentSettingsPage),
	}}}}
	oauth := capability.OAuth
	if oauth == nil || !oauth.DynamicRegistration || oauth.Issuer == "" || oauth.Scope == "" {
		return structuredText(ru,
			"Add the remote MCP server zinusya-mcp at "+capability.Resource+
				". Discover the OAuth issuer from the MCP metadata. Use its supported registration method. "+
				"If it requires pre-registration, ask the operator for the client ID and redirect URI.",
			"Добавь удалённый MCP-сервер zinusya-mcp по адресу "+capability.Resource+
				". Определи OAuth-издателя по метаданным MCP и используй поддерживаемый им способ регистрации. "+
				"Если нужна предварительная регистрация, запроси у оператора Client ID и redirect URI."), keyboard
	}
	entry := struct {
		Type  string            `json:"type"`
		URL   string            `json:"url"`
		OAuth map[string]string `json:"oauth"`
	}{"remote", capability.Resource, map[string]string{"scope": oauth.Scope}}
	example, _ := json.MarshalIndent(map[string]any{
		"mcp": map[string]any{"servers": map[string]any{externalAgentServerName: entry}},
	}, "", "  ")
	return structuredText(
		ru,
		"Add the remote MCP server zinusya-mcp to my agent. "+
			"Use Streamable HTTP and OAuth Authorization Code with PKCE S256. "+
			"Discover OAuth metadata from the MCP URL and register the client dynamically. "+
			"Register an HTTP loopback IP callback with a port and path, for example http://127.0.0.1:<port>/callback. "+
			"Open my browser for login. I will approve the client in Telegram.\n\n",
		"Добавь в мой агент удалённый MCP-сервер zinusya-mcp. "+
			"Используй Streamable HTTP и OAuth Authorization Code с PKCE S256. "+
			"Получи OAuth-метаданные по адресу MCP и зарегистрируй клиент динамически. "+
			"Укажи локальный HTTP callback с IP, портом и путём, например http://127.0.0.1:<port>/callback. "+
			"Открой браузер для входа. Я подтвержу клиент в Telegram.\n\n",
	) +
		"OAuth issuer: " + oauth.Issuer + "\nOpenCode 2:\n" + string(example), keyboard
}

func (s *Service) finishExternalAgentInstructions(
	ctx context.Context, t *db.Task, c checkpoint, i intent, text string, keyboard *telegram.InlineKeyboardMarkup,
	ru bool,
) error {
	heading := structuredText(ru, "Copy the block and send it to your agent.\n\n",
		"Скопируй блок и отправь своему агенту.\n\n")
	payload := telegram.SendMessageRequest{
		ChatID: i.ChatID, Text: heading + text, ReplyMarkup: keyboard,
		Entities: []telegram.MessageEntity{{
			Type: "pre", Offset: len(utf16.Encode([]rune(heading))), Length: len(utf16.Encode([]rune(text))),
		}},
		LinkPreviewOptions: &telegram.LinkPreviewOptions{IsDisabled: true},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	t.State = stateCompleted
	out := editCallbackOutput(i, []db.Outbound{{DedupKey: "result:structured:instructions", Payload: raw}})
	if err = s.save(ctx, t, c, out); err != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}
