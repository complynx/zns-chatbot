package task

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPairNotificationLinksInviter(t *testing.T) {
	t.Parallel()
	var notice db.PairInvitationDelivery
	notice.Kind = pairNoticePending
	notice.Context.Timezone = "UTC"
	notice.Context.InviterName = "🕺 Даниил [literal]"
	notice.Context.InviterUserID = uuid.New()
	notice.Context.Invitation.ExpiresAt = time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	input, err := pairNotificationContent(notice, "ru", telegram.User{ID: 7, FirstName: "Даниил"})
	require.NoError(t, err)
	require.NotContains(t, input.Text, notice.Context.InviterUserID.String())
	require.Contains(t, input.Text, notice.Context.InviterName)
	require.Len(t, input.Entities, 1)
	require.EqualValues(t, 7, input.Entities[0].User.ID)
	require.Equal(t, 0, input.Entities[0].Offset)
	require.NoError(t, render.Validate(render.Message{Text: input.Text, Entities: input.Entities}))
	revoked := db.User{BotID: 1, Organization: "org", TelegramUserID: 7, Revoked: true}
	mention := pairInviterMention(revoked, db.EventScope{BotID: 1, Organization: "org"}, notice.Context.InviterName)
	withoutLink, err := pairNotificationContent(notice, "ru", mention)
	require.NoError(t, err)
	require.Equal(t, input.Text, withoutLink.Text, "An unavailable mention must not suppress the notification")
	require.Empty(t, withoutLink.Entities)
}
