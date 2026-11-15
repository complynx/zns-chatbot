package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/internal/render"
)

const approvalCodeFence = "```"

// Preserve complete terms; Loom descriptions can omit long configuration or article content.
func approvalParameters(raw json.RawMessage, language string) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return ""
	}
	var out strings.Builder
	writeApprovalValue(&out, "", value, false, language)
	return out.String()
}

func approvalParameterLabel(key, language string) string {
	labels := map[string][2]string{
		"moderator_user_id":          {"Moderator", "Модератор"},
		"moderator_enabled":          {"Access enabled", "Доступ включён"},
		"reason":                     {"Reason", "Причина"},
		"user_id":                    {"Person", "Пользователь"},
		"event_id":                   {"Event", "Событие"},
		"role":                       {"Role", "Роль"},
		"title":                      {"Title", "Заголовок"},
		"body":                       {"Text", "Текст"},
		"language":                   {"Language", "Язык"},
		"sources":                    {"Sources", "Источники"},
		"content":                    {"Content", "Содержимое"},
		"decision":                   {"Decision", "Решение"},
		"amount_minor":               {"Amount in currency minor units", "Сумма в минимальных единицах валюты"},
		"expected_moderator_version": {"Current moderator assignment version", "Текущая версия назначения модератора"},
		"expected_version":           {"Current version", "Текущая версия"},
		"expected_revision":          {"Current revision", "Текущая редакция"},
		"article_id":                 {"Article", "Статья"},
		"suggestion_id":              {"Suggestion", "Предложение"},
		"specialist_id":              {"Massage specialist", "Специалист по массажу"},
		"booking_id":                 {"Massage booking", "Запись на массаж"},
		"order_id":                   {"Order", "Заказ"},
		"task_id":                    {"Task", "Задача"},
		"evidence_id":                {"Payment proof", "Подтверждение оплаты"},
		"enabled":                    {"Enabled", "Включено"},
		"name":                       {"Name", "Название"},
		"timezone":                   {"Timezone", "Часовой пояс"},
		"currency":                   {"Currency", "Валюта"},
	}
	if pair, ok := labels[key]; ok {
		if language == "ru" {
			return pair[1]
		}
		return pair[0]
	}
	return escapeMarkdownText(strconv.Quote(key))
}

func escapeMarkdownText(text string) string {
	var out strings.Builder
	for _, char := range text {
		if strings.ContainsRune("\\`*_{}[]<>()#+-.!|\"~&:/", char) {
			out.WriteByte('\\')
		}
		out.WriteRune(char)
	}
	return out.String()
}

func writeApprovalValue(out *strings.Builder, path string, value any, identity bool, language string) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			fmt.Fprintf(out, "%s: —\n", path)
		}
		for _, key := range keys {
			label := approvalParameterLabel(key, language)
			if path != "" {
				label = path + " / " + label
			}
			writeApprovalValue(out, label, v[key], key == "user_id" || strings.HasSuffix(key, "_user_id"), language)
		}
	case []any:
		if len(v) == 0 {
			fmt.Fprintf(out, "%s: —\n", path)
		}
		for n, item := range v {
			writeApprovalValue(out, fmt.Sprintf("%s %d", path, n+1), item, identity, language)
		}
	default:
		writeApprovalScalar(out, path, value, identity, language)
	}
}

func writeApprovalScalar(out *strings.Builder, path string, value any, identity bool, language string) {
	if text, ok := value.(string); ok {
		if identity && len(text) == 36 && len(render.AccountReferences(text)) == 1 {
			fmt.Fprintf(out, "%s: %s\n\n", path, text)
		} else {
			fmt.Fprintf(out, "%s:\n%s\n\n", path, literalApprovalText(text))
		}
		return
	}
	text := fmt.Sprint(value)
	if value == nil {
		text = "—"
	}
	if boolean, ok := value.(bool); ok {
		text = structuredText(language == "ru", "no", "нет")
		if boolean {
			text = structuredText(language == "ru", "yes", "да")
		}
	}
	fmt.Fprintf(out, "%s: %s\n\n", path, text)
}

func literalApprovalText(text string) string {
	if !strings.ContainsAny(text, "\r\n`") && strings.TrimSpace(text) == text && text != "" &&
		len(render.AccountReferences(text)) == 0 {
		return "`" + text + "`"
	}
	fence := approvalFence(text)
	return fence + "\n" + text + "\n" + fence
}

// One pass bounds fence construction even for long runs in article/config text.
func approvalFence(text string) string {
	longest, run := len(approvalCodeFence)-1, 0
	for n := range len(text) {
		if text[n] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", longest+1)
}
