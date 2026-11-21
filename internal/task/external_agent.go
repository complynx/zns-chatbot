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
	externalAgentEnable  = "enable"
	externalAgentDisable = "disable"
	externalAgentHistory = "history"
	externalAgentMemory  = "memory"
	externalAgentSkills  = "skills"
	externalAgentWrites  = "writes"
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
	case externalAgentEnable, externalAgentDisable, externalAgentHistory, externalAgentMemory, externalAgentSkills,
		externalAgentWrites:
		i.Kind, i.StructuredCommand, i.Text = kindStructured, externalAgentCommand, externalAgentCommand
		i.ExternalAgentChange = &externalAgentChange{Action: action, Version: version}
		return i, true, nil
	default:
		return i, true, ErrUnsupportedUpdate
	}
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
	var notice string
	if change != nil {
		profile, notice, err = s.changeExternalAgentProfile(ctx, auth, capability.Access, profile, *change, ru)
		if err != nil {
			return notice, nil
		}
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
	case externalAgentEnable:
		return loom.ExternalAgentProfile{Enabled: true, Version: c.Version}, !profile.Enabled
	case externalAgentDisable:
		return loom.ExternalAgentProfile{Version: c.Version}, profile.Enabled
	}
	if !profile.Enabled || !slices.Contains(access, c.Action) {
		return profile, false
	}
	switch c.Action {
	case externalAgentHistory:
		profile.History = !profile.History
	case externalAgentMemory:
		profile.Memory = !profile.Memory
	case externalAgentSkills:
		profile.Skills = !profile.Skills
	case externalAgentWrites:
		profile.Writes = !profile.Writes
	default:
		return profile, false
	}
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
	if !profile.Enabled {
		add(structuredText(ru, "Enable read access", "Разрешить доступ на чтение"), externalAgentEnable)
		return text + structuredText(ru, "\n\nAccess is off.", "\n\nДоступ выключен."), keyboard
	}
	text += structuredText(ru,
		"\n\nAdd this server in your MCP client and sign in with the same account:\n",
		"\n\nДобавь этот сервер в MCP-клиенте и войди в тот же аккаунт:\n") + capability.Resource
	for _, setting := range []struct {
		id, en, ru string
		enabled    bool
	}{
		{
			externalAgentHistory,
			"Loom chat history (including past results)",
			"История чата Loom (включая прошлые результаты)",
			profile.History,
		},
		{externalAgentMemory, "Personal memory", "Личная память", profile.Memory},
		{externalAgentSkills, "Organization skill library", "Библиотека навыков организации", profile.Skills},
		{externalAgentWrites, "Write and management tools", "Инструменты записи и управления", profile.Writes},
	} {
		if slices.Contains(capability.Access, setting.id) {
			prefix := structuredText(ru, "Allow: ", "Разрешить: ")
			if setting.enabled {
				prefix = structuredText(ru, "Revoke: ", "Отозвать: ")
			}
			add(prefix+structuredText(ru, setting.en, setting.ru), setting.id)
		}
	}
	add(structuredText(ru, "Turn off access and close connections", "Выключить доступ и закрыть подключения"),
		externalAgentDisable)
	return text, keyboard
}
