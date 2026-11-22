package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"unicode"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	externalAgentConnections = "connections"
	externalSelectionPrefix  = "mc"
	externalSelectionAll     = "all"
	externalSelectionRevoke  = "revoke"
	maxExternalConnections   = 16 // Match Loom's authorization limit and the callback mask width.
)

// The callback holds the selection and a fingerprint of the ordered list.
// A changed list must never reinterpret a selection as different clients.
type externalAgentSelection struct {
	snapshot string
	mask     uint16
	action   string
	index    int
}

func parseExternalAgentSelection(value string) (externalAgentSelection, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != externalSelectionPrefix || len(parts[1]) != 24 {
		return externalAgentSelection{}, false
	}
	_, hashErr := hex.DecodeString(parts[1])
	mask, maskErr := strconv.ParseUint(parts[2], 16, 16)
	if hashErr != nil || maskErr != nil {
		return externalAgentSelection{}, false
	}
	selection := externalAgentSelection{snapshot: parts[1], mask: uint16(mask), action: parts[3]}
	if selection.action == externalSelectionAll || selection.action == externalSelectionRevoke {
		return selection, true
	}
	index, err := strconv.Atoi(selection.action)
	selection.index = index
	return selection, err == nil && index >= 0 && index < maxExternalConnections
}

func externalAgentConnectionSnapshot(connections []loom.ExternalAgentConnection) string {
	hash := sha256.New()
	for _, connection := range connections {
		_, _ = hash.Write([]byte(connection.ID + "\n"))
	}
	return hex.EncodeToString(hash.Sum(nil)[:12])
}

func (s *Service) externalAgentConnectionsView(
	ctx context.Context, auth loom.Auth, version int64, selection externalAgentSelection, ru bool,
) (string, *telegram.InlineKeyboardMarkup) {
	connections, err := s.loom.ListExternalAgentConnections(ctx, auth)
	notice := ""
	var selected uint16
	if err != nil || len(connections) > maxExternalConnections {
		return externalAgentConnectionListError(version, ru)
	}
	if selection.snapshot != "" {
		all := externalAgentConnectionMask(len(connections))
		if selection.snapshot != externalAgentConnectionSnapshot(connections) || selection.mask&^all != 0 ||
			selection.index >= len(connections) {
			notice = structuredText(ru, "The list changed. Select clients again.\n\n",
				"Список изменился. Выбери клиентов заново.\n\n")
		} else {
			selected = selection.mask
			switch selection.action {
			case externalSelectionAll:
				selected = all
			case externalSelectionRevoke:
				if selected != 0 {
					return s.revokeExternalAgentConnections(ctx, auth, version, connections, selected, ru)
				}
			default:
				selected ^= 1 << selection.index
			}
		}
	}
	text, keyboard := externalAgentConnectionList(connections, version, selected, ru)
	return notice + text, keyboard
}

func (s *Service) revokeExternalAgentConnections(
	ctx context.Context, auth loom.Auth, version int64, connections []loom.ExternalAgentConnection,
	selected uint16, ru bool,
) (string, *telegram.InlineKeyboardMarkup) {
	ids := make([]string, 0, bits.OnesCount16(selected))
	for index, connection := range connections {
		if selected&(1<<index) != 0 {
			ids = append(ids, connection.ID)
		}
	}
	revokeErr := s.loom.RevokeExternalAgentConnections(ctx, auth, ids)
	connections, err := s.loom.ListExternalAgentConnections(ctx, auth)
	if err != nil || len(connections) > maxExternalConnections {
		return externalAgentConnectionListError(version, ru)
	}
	text, keyboard := externalAgentConnectionList(connections, version, 0, ru)
	if revokeErr != nil {
		return structuredText(ru, "Couldn't confirm the revocation. Check the list below.\n\n",
			"Не удалось подтвердить отзыв. Проверь список ниже.\n\n") + text, keyboard
	}
	return structuredText(ru, "Authorization revoked.\n\n", "Авторизация отозвана.\n\n") + text, keyboard
}

func externalAgentConnectionMask(count int) uint16 {
	return uint16(1<<count - 1)
}

func externalAgentConnectionList(
	connections []loom.ExternalAgentConnection, version int64, selected uint16, ru bool,
) (string, *telegram.InlineKeyboardMarkup) {
	text := structuredText(ru, "Authorized MCP clients", "Авторизованные MCP-клиенты")
	keyboard := &telegram.InlineKeyboardMarkup{}
	if len(connections) == 0 {
		text += structuredText(ru, "\n\nNo active authorizations.", "\n\nНет активных авторизаций.")
	} else {
		text += structuredText(ru, "\n\nSelect the clients whose authorization you want to revoke.",
			"\n\nВыбери клиентов, авторизацию которых хочешь отозвать.")
		snapshot := externalAgentConnectionSnapshot(connections)
		add := func(label, action string) {
			data := fmt.Sprintf("%s0:%s:%s:%x:%s", externalAgentCallbackPrefix, externalSelectionPrefix,
				snapshot, selected, action)
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []telegram.InlineKeyboardButton{{
				Text: label, CallbackData: data,
			}})
		}
		for index, connection := range connections {
			name := strings.Join(strings.FieldsFunc(connection.Name, func(r rune) bool {
				return !unicode.IsPrint(r) || unicode.IsSpace(r)
			}), " ")
			if name == "" {
				name = structuredText(ru, "Unnamed client", "Клиент без имени")
			}
			label := fmt.Sprintf("%d. %s UTC · %s", index+1, connection.CreatedAt.UTC().Format("15:04:05"), name)
			add(externalAgentToggle(selected&(1<<index) != 0, label), strconv.Itoa(index))
		}
		if selected != externalAgentConnectionMask(len(connections)) {
			add(structuredText(ru, "Select all", "Выбрать всех"), externalSelectionAll)
		}
		if selected != 0 {
			add(fmt.Sprintf(structuredText(ru, "Revoke selected (%d)", "Отозвать выбранные (%d)"),
				bits.OnesCount16(selected)), externalSelectionRevoke)
		}
	}
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, externalAgentBackButton(version, ru))
	return text, keyboard
}

func externalAgentConnectionListError(version int64, ru bool) (string, *telegram.InlineKeyboardMarkup) {
	return structuredText(ru, "Couldn't load the authorized MCP clients.",
		"Не удалось загрузить список авторизованных MCP-клиентов."), &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{externalAgentBackButton(version, ru)},
	}
}
