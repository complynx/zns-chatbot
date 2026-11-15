package task

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestMessagePeopleSkipsIAMForNonPeopleAndLiteralContent(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	actor, target := uuid.New(), uuid.New()
	_, err := store.Pool().Exec(t.Context(), `INSERT INTO shuttle_users
 (id,bot_id,tg_user_id,zitadel_subject,zitadel_org,visible,profile)
 VALUES($1,1,7,'actor','org',true,'{"first_name":"Actor"}'),
 ($2,1,8,'target','org',false,'{"first_name":"Hidden"}')`, actor, target)
	require.NoError(t, err)
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	credentials := identity.ClientCredentials{ID: "fixture", Secret: "fixture"}
	iam, err := identity.New(identity.Options{Issuer: server.URL, OrganizationID: "org",
		LoomAudience: "loom", APIAudience: "api", ActorID: "service-actor", BotID: 1,
		ActorClientCredentials: credentials, ExchangeClientCredentials: credentials,
		IntrospectionClientCredentials: credentials}, server.Client())
	require.NoError(t, err)
	s := &Service{store: store, iam: iam, logger: slog.Default()}
	i := intent{ChatID: 7, User: telegram.User{ID: 7}}
	s.messagePeople(t.Context(), i, "Event "+uuid.NewString())
	s.messagePeople(t.Context(), i, "```json\n{\"user_id\":\""+target.String()+"\"}\n```\n"+
		"    "+target.String()+"\n")
	people := s.messagePeople(t.Context(), i, actor.String()+" [Actor](tg://user?id=7)")
	require.Contains(t, people, actor.String())
	require.Zero(t, calls.Load(), "Unreplaceable literals and unrelated IDs do not require IAM")
	require.Empty(t, s.messagePeople(t.Context(), i, "Person "+target.String()))
	require.Positive(t, calls.Load(), "Genuine hidden-person references still check IAM")
}

func TestPromptReplayKeepsFirstCompletePresentation(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	ctx := t.Context()
	task, _, err := store.Intake(ctx, db.IntakeRequest{
		BotID: 1, UpdateID: 1, TelegramUserID: 7, ChatID: 7,
		Payload: json.RawMessage(`{"kind":"message","text":"hello"}`),
	}, nil)
	require.NoError(t, err)
	task, err = store.Route(ctx, task.ID)
	require.NoError(t, err)
	task, err = store.Claim(ctx, task.ID, "presentation-test", time.Minute)
	require.NoError(t, err)
	s := &Service{store: store, owner: "presentation-test", logger: slog.Default()}
	// Protocol prompts have no revision; conversational prompts can return with a
	// new revision after a question. Both must retain their first complete render.
	for _, suffix := range []string{"", ":presentation:1"} {
		out := func(count int) []db.Outbound {
			items := make([]db.Outbound, count)
			for n := range items {
				payload := telegram.SendMessageRequest{ChatID: 7, Text: "chunk " + strconv.Itoa(n)}
				if n == count-1 {
					payload.ReplyMarkup = approvalKeyboard("Approve", "Reject")
				}
				raw, marshalErr := json.Marshal(payload)
				require.NoError(t, marshalErr)
				items[n] = db.Outbound{
					DedupKey: "approval:dialog-v1:prompt:" + strconv.Itoa(n) + suffix,
					Payload:  raw,
				}
			}
			return items
		}
		require.NoError(t, s.save(ctx, &task, checkpoint{}, out(2)))
		before, readErr := store.ListOutbox(ctx, task.ID)
		require.NoError(t, readErr)
		require.NoError(t, s.save(ctx, &task, checkpoint{}, out(3)))
		after, readErr := store.ListOutbox(ctx, task.ID)
		require.NoError(t, readErr)
		require.Equal(t, before, after, "A lookup failure or renamed profile cannot append chunks/buttons")
	}
	rows, err := store.ListOutbox(ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, rows, 4, "A new presentation revision must still deliver all its chunks")
}
