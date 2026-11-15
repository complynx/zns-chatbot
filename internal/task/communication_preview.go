package task

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/complynx/zns-chatbot/internal/db"
)

const communicationPreviewUnits = 384
const communicationContactUnits = 24
const communicationPersonalContactUnits = 128
const communicationEventUnits = 64

func communicationPreviewText(c db.Communication, p db.CommunicationPreview, ru bool) string {
	event := structuredText(ru, "General messages", "Общие сообщения")
	if p.EventName != "" {
		event = communicationLabel(p.EventName, communicationEventUnits)
	}
	mode := structuredText(ru, "Literal", "Дословно")
	if c.Arguments.Personalized {
		mode = structuredText(ru, "Personalized, paid by the service", "С персонализацией за счёт сервиса")
	}
	names := make([]string, 0, len(p.Contacts))
	for _, contact := range p.Contacts {
		limit := communicationContactUnits
		if c.Category == communicationPersonal {
			limit = communicationPersonalContactUnits
		}
		name := communicationLabel(contact.Name, limit)
		if c.Category == communicationPersonal && contact.Username != "" && contact.Name != "@"+contact.Username {
			name += " (@" + contact.Username + ")"
		}
		names = append(names, name)
	}
	recipients := strings.Join(names, ", ")
	if rest := len(c.Arguments.Recipients) - len(names); rest > 0 {
		recipients += fmt.Sprintf(structuredText(ru, " and %d more", " и ещё %d"), rest)
	}
	return communicationLabelLines(structuredText(ru, "Event: ", "Мероприятие: ")+event+"\n"+
		structuredText(
			ru,
			"Mode: ",
			"Режим: ",
		)+mode+"\n"+structuredText(ru, "To: ", "Кому: ")+recipients, communicationPreviewUnits)
}

func communicationLabel(text string, limit int) string {
	return communicationLabelLines(strings.Join(strings.Fields(text), " "), limit)
}

// Limits are in Telegram UTF-16 units; labels cannot split an emoji in half.
func communicationLabelLines(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(utf16.Encode([]rune(text))) <= limit {
		return text
	}
	var result strings.Builder
	units := 0
	for _, r := range text {
		next := utf16.RuneLen(r)
		if units+next > limit-1 {
			break
		}
		result.WriteRune(r)
		units += next
	}
	return result.String() + "…"
}
