package sandbox

import (
	"maps"
	"net/http"
	"slices"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const setMenuButtonMethod = "setChatMenuButton"
const setCommandsMethod = "setMyCommands"
const maxMenuCalls = 64

type menuCall struct {
	Method       string `json:"method"`
	LanguageCode string `json:"language_code"`
	Applied      bool   `json:"applied"`
}
type telegramMenuState struct {
	TotalCalls uint64                           `json:"total_calls"`
	Button     *telegram.MenuButton             `json:"menu_button"`
	Commands   map[string][]telegram.BotCommand `json:"commands"`
	Calls      []menuCall                       `json:"recent_calls"`
}
type menuFault struct {
	Method       string `json:"method"`
	LanguageCode string `json:"language_code"`
}
type menuRequest struct {
	ChatID     int64                `json:"chat_id,omitempty"`
	MenuButton *telegram.MenuButton `json:"menu_button,omitempty"`
	Scope      *struct {
		Type string `json:"type"`
	} `json:"scope,omitempty"`
	LanguageCode string                `json:"language_code,omitempty"`
	Commands     []telegram.BotCommand `json:"commands,omitempty"`
}

func (f *Fake) menuRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /lab/telegram-menu", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		api.JSON(w, http.StatusOK, f.menu)
	})
	mux.HandleFunc("POST /lab/menu-fault", func(w http.ResponseWriter, r *http.Request) {
		if !labRequest(w, r) {
			return
		}
		var fault menuFault
		if api.Decode(w, r, &fault) != nil ||
			(fault.Method != setMenuButtonMethod && fault.Method != setCommandsMethod) ||
			!validMenuLanguage(fault.LanguageCode) {
			api.JSON(w, http.StatusBadRequest, nil)
			return
		}
		f.mu.Lock()
		f.menuFailure = &fault
		f.mu.Unlock()
		api.JSON(w, http.StatusOK, fault)
	})
}

func (f *Fake) telegramMenu(w http.ResponseWriter, r *http.Request, method string) {
	var request menuRequest
	if api.Decode(w, r, &request) != nil || request.ChatID != 0 ||
		request.Scope != nil && request.Scope.Type != "default" ||
		!validMenuLanguage(request.LanguageCode) {
		tgError(w, http.StatusBadRequest, "invalid default menu request")
		return
	}
	if method == setMenuButtonMethod && (request.MenuButton == nil || request.MenuButton.Type != "commands") ||
		method == setCommandsMethod && !validMenuCommands(request.Commands) {
		tgError(w, http.StatusBadRequest, "invalid menu payload")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if method == "getChatMenuButton" {
		button := f.menu.Button
		if button == nil {
			button = &telegram.MenuButton{Type: "default"}
		}
		tgOK(w, button)
		return
	}
	if method == "getMyCommands" {
		commands := f.menu.Commands[request.LanguageCode]
		if commands == nil {
			commands = []telegram.BotCommand{}
		}
		tgOK(w, commands)
		return
	}
	f.applyTelegramMenu(w, r, method, request)
}

// Caller holds the fake state lock across persistence and acknowledgement.
func (f *Fake) applyTelegramMenu(w http.ResponseWriter, r *http.Request, method string, request menuRequest) {
	applied := true
	if f.menuFailure != nil && f.menuFailure.Method == method && f.menuFailure.LanguageCode == request.LanguageCode {
		applied = false
		f.menuFailure = nil
	}
	before := f.menu
	next := telegramMenuState{
		TotalCalls: before.TotalCalls + 1,
		Button:     before.Button,
		Commands:   maps.Clone(before.Commands),
		Calls:      slices.Clone(before.Calls),
	}
	next.Calls = append(next.Calls, menuCall{Method: method, LanguageCode: request.LanguageCode, Applied: applied})
	if len(next.Calls) > maxMenuCalls {
		next.Calls = next.Calls[len(next.Calls)-maxMenuCalls:]
	}
	if applied {
		if method == setMenuButtonMethod {
			next.Button = request.MenuButton
		} else {
			if next.Commands == nil {
				next.Commands = make(map[string][]telegram.BotCommand)
			}
			next.Commands[request.LanguageCode] = request.Commands
		}
	}
	f.menu = next
	if err := f.save(r.Context()); err != nil {
		f.menu = before
		tgError(w, http.StatusServiceUnavailable, "state unavailable")
		return
	}
	if !applied {
		tgError(w, http.StatusTooManyRequests, "Too Many Requests")
		return
	}
	tgOK(w, true)
}

func validMenuLanguage(language string) bool {
	return language == "" ||
		len(language) == 2 && language[0] >= 'a' && language[0] <= 'z' && language[1] >= 'a' && language[1] <= 'z'
}
func validMenuCommands(commands []telegram.BotCommand) bool {
	const maxCommands = 100
	const maxName = 32
	const maxDescription = 256
	if len(commands) > maxCommands {
		return false
	}
	for _, command := range commands {
		if len(command.Command) == 0 || len(command.Command) > maxName || !utf8.ValidString(command.Description) ||
			utf8.RuneCountInString(command.Description) == 0 ||
			utf8.RuneCountInString(command.Description) > maxDescription {
			return false
		}
		for _, char := range command.Command {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
				return false
			}
		}
	}
	return true
}
