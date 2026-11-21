package task

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const passCallbackPrefix = "ps:"

type passInput struct {
	Event           string `json:"event,omitempty"`
	Code            string `json:"code,omitempty"`
	Role            string `json:"role,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Details         []byte `json:"details,omitempty"`
	DetailsDisabled bool   `json:"details_disabled,omitempty"`
	Invalid         bool   `json:"invalid,omitempty"`
}

func isPassCommand(text string) bool {
	fields := strings.Fields(text)
	return len(fields) > 0 && fields[0] == passesCommand
}

// passWords recognizes double quotes for fields such as a full legal name.
func passWords(text string) ([]string, bool) {
	var words []string
	var word strings.Builder
	quoted, started := false, false
	for _, r := range text {
		switch {
		case r == '"':
			quoted, started = !quoted, true
		case !quoted && unicode.IsSpace(r):
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if started {
		words = append(words, word.String())
	}
	return words, !quoted
}

func parsePassInput(text string) (passInput, []registrationdetails.Mutation) {
	words, ok := passWords(text)
	if !ok || len(words) == 0 || words[0] != passesCommand {
		return passInput{Invalid: true}, nil
	}
	var input passInput
	var details []registrationdetails.Mutation
	seen := make(map[string]bool)
	for n := 1; n < len(words); n++ {
		key := words[n]
		switch key {
		case "--solo", "--pair":
			if input.Mode != "" {
				return passInput{Invalid: true}, nil
			}
			input.Mode = strings.TrimPrefix(key, "--")
			continue
		case "--invite", "--role", "--legal-name", "--passport-number", "--passport-country":
			if seen[key] || n+1 == len(words) || words[n+1] == "" || strings.HasPrefix(words[n+1], "--") {
				return passInput{Invalid: true}, nil
			}
			seen[key] = true
			n++
		default:
			if input.Event != "" || !passIdentifier(key) {
				return passInput{Invalid: true}, nil
			}
			input.Event = key
			continue
		}
		value := words[n]
		switch key {
		case "--invite":
			input.Code = value
		case "--role":
			input.Role = strings.ToLower(value)
		default:
			field := registrationdetails.Field(strings.ReplaceAll(strings.TrimPrefix(key, "--"), "-", "_"))
			details = append(details, registrationdetails.Mutation{Field: field, Value: value})
		}
	}
	if !input.valid() {
		return passInput{Invalid: true}, nil
	}
	return input, details
}

func (i passInput) valid() bool {
	return (i.Code == "" || (passIdentifier(i.Code) && i.Mode == "")) &&
		(i.Role == "" || registrations.ValidRole(i.Role))
}

func passIdentifier(value string) bool {
	if value == "" || len(value) > 64 || value[0] == '-' {
		return false
	}
	for _, r := range value {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if !letter && !digit && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// Before durable intake, separate and encrypt private values. The normal raw
// Telegram update is also persisted, so remove the original command from it.
func (s *Service) preparePassInput(i *intent) error {
	raw, err := redactPassUpdate(i.RawUpdate)
	if err != nil {
		return err
	}
	i.RawUpdate = raw
	public, hasArguments := publicPassCommand(i.Text)
	if public == "" || i.PassCallback != "" {
		return nil
	}
	command := i.Text
	if i.StructuredCommand == startCommand {
		command = startPayload(command)
	}
	if i.Kind != kindStructured || !isPassCommand(command) {
		if hasArguments {
			// A forwarded or decorated command is not an instruction from its owner.
			i.Text, i.Kind, i.StructuredCommand = public, kindUnsupported, ""
			i.Symbols, i.Attachments = nil, nil
		}
		return nil
	}
	i.Text = public
	input, details := parsePassInput(command)
	if err = s.protectPassInput(i.User.ID, &input, details); err != nil {
		return err
	}
	i.PassInput = &input
	return nil
}

// Recognize addressed commands for privacy even when the native handler only
// accepts the unqualified spelling.
func publicPassCommand(text string) (string, bool) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return "", false
	}
	prefix := ""
	command, _, _ := strings.Cut(words[0], "@")
	if command == startCommand && len(words) > 1 {
		prefix = startCommand + " "
		words = words[1:]
		command, _, _ = strings.Cut(words[0], "@")
	}
	if command == passesCommand {
		return prefix + passesCommand, len(words) > 1
	}
	return "", false
}

func (s *Service) protectPassInput(userID int64, input *passInput, details []registrationdetails.Mutation) error {
	if len(details) == 0 {
		return nil
	}
	protector := s.options.RegistrationDetailsProtector
	if protector == nil {
		input.DetailsDisabled = true
		return nil
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	input.Details, err = protector.Seal(s.passInputOwner(userID), string(raw))
	return err
}

// Keep only message coordinates and safe text when a Telegram update carries
// a pass command. Other fields, including entities and previews, can echo it.
type passMessageExcerpt struct {
	ID      int64               `json:"message_id,omitempty"`
	Text    string              `json:"text,omitempty"`
	Caption string              `json:"caption,omitempty"`
	Reply   *passMessageExcerpt `json:"reply_to_message,omitempty"`
	Quote   *passMessageExcerpt `json:"quote,omitempty"`
	Pinned  *passMessageExcerpt `json:"pinned_message,omitempty"`
}

func (m *passMessageExcerpt) redact() bool {
	if m == nil {
		return false
	}
	text, _ := publicPassCommand(m.Text)
	caption, _ := publicPassCommand(m.Caption)
	echo := m.Reply.redact() || m.Quote.redact() || m.Pinned.redact()
	if text == "" && caption == "" && !echo {
		return false
	}
	if text != "" || caption != "" {
		m.Text, m.Caption = text, caption
	}
	if m.Reply != nil {
		m.Reply = &passMessageExcerpt{ID: m.Reply.ID}
	}
	m.Quote, m.Pinned = nil, nil
	return true
}

func redactPassUpdate(raw json.RawMessage) (json.RawMessage, error) {
	var update struct {
		ID       int64               `json:"update_id"`
		Message  *passMessageExcerpt `json:"message,omitempty"`
		Callback *struct {
			ID      string              `json:"id"`
			Data    string              `json:"data"`
			Message *passMessageExcerpt `json:"message"`
		} `json:"callback_query,omitempty"`
	}
	if json.Unmarshal(raw, &update) != nil {
		return nil, ErrUnsupportedUpdate
	}
	if !update.Message.redact() && (update.Callback == nil || !update.Callback.Message.redact()) {
		return raw, nil
	}
	return json.Marshal(update)
}

func (s *Service) passInputOwner(userID int64) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("telegram-pass-command:%d:%d", s.options.BotID, userID)))
}

func (s *Service) passInviteAtIntake(i intent) *db.PassInviteInput {
	input := i.PassInput
	if input == nil || input.Invalid || input.DetailsDisabled || input.Mode != "" ||
		(input.Code == "" && input.Event == "") {
		return nil
	}
	return &db.PassInviteInput{Organization: s.notificationScope().Organization,
		Event: input.Event, Code: input.Code, Role: input.Role}
}

func normalizePassCallback(i intent, cb *telegram.CallbackQuery) (intent, bool, error) {
	data, ok := strings.CutPrefix(cb.Data, passCallbackPrefix)
	if !ok {
		return i, false, nil
	}
	prompt, choice, ok := strings.Cut(data, ":")
	id, err := uuid.Parse(prompt)
	if !ok || err != nil || id == uuid.Nil || id.String() != prompt || cb.Message.ID <= 0 ||
		len(choice) != 1 || choice[0] < '0' || choice[0] > '9' {
		return i, true, ErrUnsupportedUpdate
	}
	i.Kind, i.StructuredCommand, i.Text = kindStructured, passesCommand, passesCommand
	i.CallbackMessageID, i.CallbackTaskID, i.PassCallback = cb.Message.ID, id, choice
	return i, true, nil
}
