package task

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestApprovalOutputNamesDebugAndFinalButtons(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	actor, target := uuid.New(), uuid.New()
	_, err := store.Pool().Exec(t.Context(), `INSERT INTO shuttle_users
 (id,bot_id,tg_user_id,zitadel_subject,zitadel_org,profile)
 VALUES($1,1,7,'actor','org','{"first_name":"Actor"}'),
 ($2,1,8,'target','org','{"first_name":"Даниил","last_name":"Дрижук"}')`, actor, target)
	require.NoError(t, err)
	credentials := identity.ClientCredentials{ID: "fixture", Secret: "fixture"}
	iam, err := identity.New(identity.Options{Issuer: "https://iam.invalid", OrganizationID: "org",
		LoomAudience: "loom", APIAudience: "api", ActorID: "actor", BotID: 1,
		ActorClientCredentials: credentials, ExchangeClientCredentials: credentials,
		IntrospectionClientCredentials: credentials}, nil)
	require.NoError(t, err)
	s := &Service{store: store, iam: iam, logger: slog.Default()}
	i := intent{ChatID: 7, User: telegram.User{ID: 7, FirstName: "Actor", LanguageCode: "ru"}}
	a := loom.Approval{
		ID:        "approval",
		Method:    "zinusya_knowledge.set_moderator",
		ReplyMode: loom.ReplyModeProtocol,
		Text:      strings.Repeat("Context ", 700) + "Moderator " + target.String(),
		Arguments: json.RawMessage(
			`{"moderator_user_id":"` + target.String() + `","moderator_enabled":true,"reason":"**literal**"}`,
		),
	}
	for _, debug := range []bool{false, true} {
		s.options.DebugApprovalJSON = debug
		out := s.approval(t.Context(), i, uuid.New(), a)
		require.Greater(t, len(out), 1)
		var text strings.Builder
		mentions := 0
		for n, item := range out {
			var payload telegram.SendMessageRequest
			require.NoError(t, json.Unmarshal(item.Payload, &payload))
			require.NoError(t, render.Validate(render.Message{Text: payload.Text, Entities: payload.Entities}))
			text.WriteString(payload.Text)
			if n == len(out)-1 {
				require.NotNil(t, payload.ReplyMarkup)
			} else {
				require.Nil(t, payload.ReplyMarkup)
			}
			for _, entity := range payload.Entities {
				if entity.Type == "text_mention" {
					mentions++
					require.EqualValues(t, 8, entity.User.ID)
				}
			}
		}
		require.Equal(t, 2, mentions)
		require.Contains(t, text.String(), "Даниил Дрижук")
		require.Contains(t, text.String(), "**literal**")
		if debug {
			require.Contains(t, text.String(), `"moderator_user_id": "`+target.String()+`"`)
		} else {
			require.NotContains(t, text.String(), target.String())
			require.NotContains(t, text.String(), "moderator_user_id")
		}
	}
}

func TestPairAssignmentApprovalLinksPeopleAndKeepsTerms(t *testing.T) {
	t.Parallel()
	leader, follower := uuid.New(), uuid.New()
	description := "Leader account " + leader.String() + "; follower account " + follower.String() + ". Price 100 EUR."
	view := db.PairAssignmentProposalView{
		State:       db.AssignmentProposalPending,
		Preparation: db.PairAssignmentPreparation{Description: description},
		Leader:      &db.PairAssignmentParticipant{UserID: leader, TelegramUserID: 7, DisplayName: "🕺 Даниил"},
		Follower:    &db.PairAssignmentParticipant{UserID: follower, TelegramUserID: 8, DisplayName: "**Literal Name**"},
	}
	text, err := fullPairAssignmentApproval(view, time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC), "ru")
	require.NoError(t, err)
	verified := map[int64]telegram.User{7: {ID: 7, FirstName: "🕺 Даниил"}, 8: {ID: 8, FirstName: "**Literal Name**"}}
	chunks := render.People(text, verified, map[string]telegram.User{
		leader.String():   {ID: 7, FirstName: "🕺 Даниил"},
		follower.String(): {ID: 8, FirstName: "**Literal Name**"},
	})
	require.Len(t, chunks, 1)
	require.NotContains(t, chunks[0].Text, leader.String())
	require.NotContains(t, chunks[0].Text, follower.String())
	require.Contains(t, chunks[0].Text, "Price 100 EUR.")
	require.Contains(t, chunks[0].Text, "**Literal Name**")
	mentions := 0
	for _, entity := range chunks[0].Entities {
		if entity.Type == "text_mention" {
			mentions++
		}
	}
	require.Equal(t, 2, mentions)
	require.Equal(t, description, view.Preparation.Description)
	require.NoError(t, render.Validate(chunks[0]))
	plain := render.Render(text, nil)
	require.Contains(t, plain[0].Text, "🕺 Даниил", "Names must survive an unavailable identity lookup")
	require.Contains(t, plain[0].Text, "**Literal Name**")
	view.Follower.DisplayName = ""
	unnamed, err := fullPairAssignmentApproval(view, time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC), "ru")
	require.NoError(t, err)
	require.NotContains(t, unnamed, `account ""`)
	require.Contains(t, unnamed, follower.String(), "Keep the locator when there is no display name")
}

func TestMixedPersonReferencesAreAlreadyResolved(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	people := map[string]telegram.User{id.String(): {ID: 7, FirstName: "Даниил"}}
	require.True(t, messagePeopleResolved(people, []uuid.UUID{id}, []int64{7}))
	require.False(t, messagePeopleResolved(people, []uuid.UUID{id}, []int64{8}))
}
