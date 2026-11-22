package task

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const externalAgentCommand = "/mcp"
const externalAgentCallbackPrefix = "mcp:"

const (
	externalAgentLegacyEnable = "enable"
	externalAgentEnableRead   = "read-on"
	externalAgentDisable      = "disable"
	externalAgentHistory      = "history"
	externalAgentMemory       = "memory"
	externalAgentSkills       = "skills"
	externalAgentWrites       = "writes"
	externalAgentInstructions = "instructions"
	externalAgentSettingsPage = "settings"
)

type externalAgentChange struct {
	Action  string `json:"action"`
	Version int64  `json:"version"`
}

func externalAgentCallback(i intent, data string) (intent, bool, error) {
	value, matched := strings.CutPrefix(data, externalAgentCallbackPrefix)
	if !matched {
		return i, false, nil
	}
	rawVersion, action, ok := strings.Cut(value, ":")
	version, err := strconv.ParseInt(rawVersion, 10, 64)
	if !ok || err != nil || version < 0 || strconv.FormatInt(version, 10) != rawVersion {
		return i, true, ErrUnsupportedUpdate
	}
	switch action {
	case externalAgentLegacyEnable, externalAgentEnableRead, externalAgentDisable,
		externalAgentHistory, externalAgentMemory, externalAgentSkills, externalAgentWrites,
		externalAgentInstructions, externalAgentSettingsPage, externalAgentConnections:
	default:
		if _, valid := parseExternalAgentSelection(action); !valid || i.CallbackMessageID <= 0 {
			return i, true, ErrUnsupportedUpdate
		}
	}
	i.Kind, i.StructuredCommand, i.Text = kindStructured, externalAgentCommand, externalAgentCommand
	i.ExternalAgentChange = &externalAgentChange{Action: action, Version: version}
	return i, true, nil
}

func (s *Service) structuredExternalAgent(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	ru := s.languageFor(ctx, i) == "ru"
	if !s.options.ExternalAgentEnabled {
		return s.finishStructuredKeyboard(ctx, t, c, i, externalAgentUnavailable(ru), nil, ru)
	}
	user, service, err := s.authorize(ctx, *t, i)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	_, subject, err := s.taskIdentity(ctx, user)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	auth := loom.Auth{ServiceToken: service.Token, SubjectToken: subject.Token}
	message, keyboard := s.externalAgentView(ctx, auth, i.ExternalAgentChange, ru)
	if i.ExternalAgentChange != nil && i.ExternalAgentChange.Action == externalAgentInstructions &&
		keyboard != nil {
		return s.finishExternalAgentInstructions(ctx, t, c, i, message, keyboard, ru)
	}
	return s.finishStructuredKeyboard(ctx, t, c, i, message, keyboard, ru)
}

func externalAgentUnavailable(ru bool) string {
	return structuredText(ru,
		"External agents are currently unavailable.",
		"Подключение внешних агентов сейчас недоступно.")
}

func (s *Service) externalAgentView(
	ctx context.Context, auth loom.Auth, change *externalAgentChange, ru bool,
) (string, *telegram.InlineKeyboardMarkup) {
	capability, err := s.loom.ExternalAgent(ctx, auth)
	if err != nil {
		return externalAgentUnavailable(ru), nil
	}
	profile, err := s.loom.ExternalAgentProfile(ctx, auth)
	if err != nil {
		return externalAgentUnavailable(ru), nil
	}
	if !s.options.ExternalAgentWrites && !profile.Writes {
		capability.Access = slices.DeleteFunc(capability.Access, func(access string) bool {
			return access == externalAgentWrites
		})
	}
	if change == nil || change.Action == externalAgentSettingsPage {
		return externalAgentSettings(*capability, profile, ru)
	}
	if change.Action == externalAgentInstructions {
		return externalAgentInstructionsView(*capability, profile, ru)
	}
	selection, selected := parseExternalAgentSelection(change.Action)
	if change.Action == externalAgentConnections || selected {
		if capability.OAuth == nil || !capability.OAuth.ManageConnections {
			return externalAgentSettings(*capability, profile, ru)
		}
		return s.externalAgentConnectionsView(ctx, auth, profile.Version, selection, ru)
	}
	var notice string
	if change.Version == profile.Version && change.Action == externalAgentWrites && !profile.Enabled {
		notice = structuredText(ru, "Enable read access first.\n\n", "Сначала включи чтение.\n\n")
	} else {
		profile, notice, err = s.changeExternalAgentProfile(ctx, auth, capability.Access, profile, *change, ru)
	}
	if err != nil {
		return notice, nil
	}
	text, keyboard := externalAgentSettings(*capability, profile, ru)
	return notice + text, keyboard
}

func (s *Service) changeExternalAgentProfile(
	ctx context.Context,
	auth loom.Auth,
	access []string,
	profile loom.ExternalAgentProfile,
	change externalAgentChange,
	ru bool,
) (loom.ExternalAgentProfile, string, error) {
	const oldSettingsEN = "That button is out of date. Check the current settings below.\n\n"
	const oldSettingsRU = "Эта кнопка устарела. Проверь текущие настройки ниже.\n\n"
	desired, ok := change.apply(profile, access)
	if !ok {
		return profile, structuredText(ru, oldSettingsEN, oldSettingsRU), nil
	}
	updated, err := s.loom.PutExternalAgentProfile(ctx, auth, desired)
	if err == nil {
		return updated, "", nil
	}
	// Read back the profile after a conflict or a lost response. Never apply this
	// choice to a newer version, even when retrying the same Telegram update.
	updated, err = s.loom.ExternalAgentProfile(ctx, auth)
	if err != nil {
		return updated, structuredText(ru,
			"I couldn't verify the current settings. Open /mcp to check them.",
			"Не удалось проверить настройки. Открой /mcp, чтобы проверить их."), err
	}
	desired.Version++
	if updated == desired {
		return updated, "", nil
	}
	if updated.Version != profile.Version {
		return updated, structuredText(ru, oldSettingsEN, oldSettingsRU), nil
	}
	return updated, structuredText(ru,
		"I couldn't save the change.\n\n", "Не удалось сохранить изменение.\n\n"), nil
}

func (c externalAgentChange) apply(
	profile loom.ExternalAgentProfile, access []string,
) (loom.ExternalAgentProfile, bool) {
	if c.Version != profile.Version {
		return profile, false
	}
	switch c.Action {
	case externalAgentEnableRead:
		return loom.ExternalAgentProfile{
			Enabled: true, Version: c.Version,
			History: slices.Contains(access, externalAgentHistory),
			Memory:  slices.Contains(access, externalAgentMemory),
			Skills:  slices.Contains(access, externalAgentSkills),
		}, !profile.Enabled
	case externalAgentDisable:
		return loom.ExternalAgentProfile{Version: c.Version}, profile.Enabled
	}
	if !profile.Enabled || !slices.Contains(access, c.Action) {
		return profile, false
	}
	if c.Action != externalAgentWrites {
		return profile, false
	}
	profile.Writes = !profile.Writes
	return profile, true
}

func externalAgentSettings(
	capability loom.ExternalAgentCapability, profile loom.ExternalAgentProfile, ru bool,
) (string, *telegram.InlineKeyboardMarkup) {
	keyboard := &telegram.InlineKeyboardMarkup{}
	add := func(label, action string) {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []telegram.InlineKeyboardButton{{
			Text: label, CallbackData: fmt.Sprintf("%s%d:%s", externalAgentCallbackPrefix, profile.Version, action),
		}})
	}
	text := structuredText(ru,
		"External agents (MCP)\n\nA connected agent can read data you allow and send it outside Loom.",
		"Внешние агенты (MCP)\n\nПодключённый агент может читать разрешённые данные и передавать их за пределы Loom.")
	if slices.Contains(capability.TokenServices, externalApprovalService) {
		text += structuredText(ru,
			" Zinusya tools follow your current roles, including administrative permissions.",
			" Инструменты Зинуси учитывают твои текущие права, включая административные.")
	}
	text += structuredText(ru,
		"\n\nTurning on read access allows available chat history, personal memory and organization skills. "+
			"Turning it off also disables changes and closes existing connections.",
		"\n\nВключение чтения разрешает доступ к истории чатов, личной памяти и навыкам организации, если они доступны. "+
			"Выключение чтения также отключает изменения и закрывает подключения.")
	readAction := externalAgentEnableRead
	if profile.Enabled {
		readAction = externalAgentDisable
	}
	add(externalAgentToggle(profile.Enabled, structuredText(ru, "Read", "Чтение")), readAction)
	if slices.Contains(capability.Access, externalAgentWrites) {
		add(externalAgentToggle(profile.Writes, structuredText(ru, "Changes", "Изменения")), externalAgentWrites)
	}
	add(structuredText(ru, "Get instructions for the agent", "Получить инструкцию для агента"), externalAgentInstructions)
	if capability.OAuth != nil && capability.OAuth.ManageConnections {
		add(structuredText(ru, "Authorized clients", "Авторизованные клиенты"), externalAgentConnections)
	}
	if profile.Enabled {
		add(structuredText(ru, "Disable", "Отключить"), externalAgentDisable)
	}
	text += structuredText(ru,
		"\n\nMCP server: ", "\n\nСервер MCP: ") + capability.Resource
	return text, keyboard
}

func externalAgentToggle(enabled bool, label string) string {
	if enabled {
		return "☑️ " + label
	}
	return "⬜ " + label
}
