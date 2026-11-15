package task

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
	"github.com/complynx/zns-chatbot/internal/db"
)

func TestCommunicationConfirmationShowsRecipientsWithinTelegramLimit(t *testing.T) {
	t.Parallel()
	c := db.Communication{Category: "warning", Arguments: db.CommunicationArguments{
		Text: strings.Repeat(
			"😀",
			1400,
		),
		Assessment:       strings.Repeat("😀", 256),
		AssessedCategory: broadcasts.Information,
		Personalized:     true,
	}}
	p := db.CommunicationPreview{EventName: strings.Repeat("Event ", 100)}
	for range 5 {
		p.Contacts = append(p.Contacts, db.CommunicationContact{Name: strings.Repeat("😀", 100)})
		c.Arguments.Recipients = append(c.Arguments.Recipients, uuid.New())
	}
	c.Arguments.Recipients = append(c.Arguments.Recipients, uuid.New())
	body, _ := communicationConfirmation(c, true)
	text := communicationPreviewText(c, p, true) + "\n\n" + body
	require.LessOrEqual(t, len(utf16.Encode([]rune(text))), 4096)
	require.Contains(t, text, "и ещё 1")
	require.Contains(t, text, "Event")
	require.Equal(t, "First Last", communicationLabel("First\nLast", communicationPersonalContactUnits))
	c.Arguments.Personalized = false
	c.Category = "personal"
	c.Arguments.Recipients = c.Arguments.Recipients[:1]
	p.Contacts = []db.CommunicationContact{{Name: "Visible Recipient", Username: "recipient"}}
	text = communicationPreviewText(c, p, false)
	require.Contains(t, text, "Visible Recipient")
	require.Contains(t, text, "@recipient")
}
