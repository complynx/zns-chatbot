package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminBroadcastManualInputDoesNotTrap(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
			require.NoError(t, err)
			handleVisible(t, f.b, message(7100, 202, "/send_message_to 101"))
			var inputID, prompt int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT id,prompt_id FROM core.admin_message_inputs WHERE actor='bob'`).
					Scan(&inputID, &prompt),
			)
			require.Positive(t, prompt)
			promptCard := adminRuntimeControl(t, f, fmt.Sprintf("adminmsg:inputcancel:%d", inputID))
			require.Equal(t, prompt, promptCard.ID)
			require.Empty(t, chatMessages(t, f, 101))
			before := f.model.calls
			handleVisible(t, f.b, message(7101, 202, "What is two plus two?"))
			assert.Greater(t, f.model.calls, before)
			var retainedPrompt, retainedDraft int64
			require.NoError(t, f.db.QueryRow(t.Context(),
				"SELECT prompt_id,COALESCE(message_id,0) FROM core.admin_message_inputs WHERE id=$1",
				inputID).Scan(&retainedPrompt, &retainedDraft))
			require.Equal(t, prompt, retainedPrompt)
			require.Zero(t, retainedDraft, "unrelated text must not become a draft")
			require.Empty(t, chatMessages(t, f, 101))
			update := message(7102, 202, "Hello <Alice>")
			update.Message.ReplyToMessage = &promptCard
			update.Message.Entities = []telegram.MessageEntity{{Type: "bold", Offset: 0, Length: 5}}
			handleVisible(t, f.b, update)
			var draftID int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT message_id FROM core.admin_message_inputs WHERE id=$1`, inputID).
					Scan(&draftID),
			)
			page, err := (adminmessage.Service{DB: f.db}).Review(t.Context(), "bob", draftID, 0)
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.Equal(t, "<b>Hello</b> &lt;Alice&gt;", page.Items[0].Content.Text)
			assert.Equal(t, "HTML", page.Items[0].Content.ParseMode)
			require.Empty(t, chatMessages(t, f, 101))
		})
	}
}

func TestAdminBroadcastScriptGroundingAndRevocation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING`,
	)
	require.NoError(t, err)
	service := adminmessage.Service{DB: f.db}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) SELECT id,jsonb_build_object('user_id',telegram_id,'first_name',name) FROM core.users ON CONFLICT(owner) DO NOTHING`,
	)
	require.NoError(t, err)
	pending, err := service.BeginInput(t.Context(), "alice", "agent-input", "/send_message_to 202 --forward", 101)
	require.NoError(t, err)
	require.NoError(t, service.RegisterPrompt(t.Context(), "alice", pending.ID, 101, 7000))
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			names, encodeErr := json.Marshal(tools)
			require.NoError(t, encodeErr)
			assert.Contains(t, string(names), "broadcasts.attach")
			assert.Contains(t, string(names), "broadcasts.audience")
			assert.Contains(t, string(names), "broadcasts.profile")
			audience := scriptCall(ctx, t, callback, "broadcasts.audience", `{}`)
			assert.Contains(t, string(audience), `"user_id":"202"`)
			profile := scriptCall(ctx, t, callback, "broadcasts.profile", `{"user_id":"202"}`)
			assert.Contains(t, string(profile), "first_name")
			forged := fmt.Sprintf(`{"input_id":%d,"from_chat":999}`, pending.ID)
			_, callErr := callback(
				ctx,
				scriptclient.ToolCall{Name: "broadcasts.attach", Arguments: json.RawMessage(forged)},
			)
			require.Error(t, callErr)
			result := scriptCall(ctx, t, callback, "broadcasts.attach", fmt.Sprintf(`{"input_id":%d}`, pending.ID))
			assert.Contains(t, string(result), "confirmation_required")
			_, removeErr := f.db.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
			require.NoError(t, removeErr)
			list := scriptCall(ctx, t, callback, "$list", "{}")
			assert.NotContains(t, string(list), "broadcasts.")
			_, callErr = callback(
				ctx,
				scriptclient.ToolCall{Name: "broadcasts.pending", Arguments: json.RawMessage(`{}`)},
			)
			require.Error(t, callErr)
			_, callErr = callback(
				ctx,
				scriptclient.ToolCall{Name: "broadcasts.profile", Arguments: json.RawMessage(`{"user_id":"202"}`)},
			)
			require.Error(t, callErr)
			return json.RawMessage(`{"previewed":true}`), nil
		},
	)
	f.b.Model = &knowledgeModel{
		plans: []agent.Plan{
			{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
			{View: agent.OrdersView, Text: "Preview ready"},
		},
	}
	handle(t, f.b, message(7103, 101, "Use this message for the pending broadcast"))
	var content adminmessage.Content
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT r.content FROM core.admin_message_recipients r JOIN core.admin_messages m ON m.id=r.message_id WHERE m.actor='alice'`).
			Scan(&content),
	)
	assert.EqualValues(t, 101, content.FromChat)
	assert.EqualValues(t, 7103, content.FromMessage)
	var deliveries int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_message_deliveries`).Scan(&deliveries),
	)
	assert.Zero(t, deliveries)
}

func TestAdminBroadcastScriptNamesHidden(t *testing.T) {
	t.Parallel()
	f := setup(t)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				names, err := json.Marshal(tools)
				require.NoError(t, err)
				assert.NotContains(t, string(names), "broadcasts.")
				list := scriptCall(ctx, t, callback, "$list", "{}")
				assert.NotContains(t, string(list), "broadcasts.")
				_, err = callback(
					ctx,
					scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"broadcasts.preview"}`)},
				)
				require.Error(t, err)
				_, err = callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "broadcasts.preview",
						Arguments: json.RawMessage(`{"command":"/send_message_to 202 --msg test"}`),
					},
				)
				require.Error(t, err)
				return json.RawMessage(`true`), nil
			},
		),
	)
}

func TestAdminBroadcastUserCannotForgeSource(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	for _, test := range []struct {
		path, body string
		status     int
	}{
		{"/internal/admin-messages/source", `{"actor":"bob","key":"forged","chat_id":999,"message_id":5,"html":"stolen"}`, http.StatusUnauthorized},
		{"/v1/admin-messages/input/attach", `{"input_id":1,"chat_id":999,"prompt_id":2,"key":"forged","content":{"from_chat":999,"from_message":5}}`, http.StatusBadRequest},
	} {
		request, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			f.b.API.Base+test.path,
			strings.NewReader(test.body),
		)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+f.b.Host.Signer.Token("bob"))
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, test.status, response.StatusCode)
	}
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_message_sources`).Scan(&count))
	assert.Zero(t, count)
}

func TestAdminBroadcastManualHelpRequiresCurrentRole(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(7400, 101, "/send_message_to"))
	cards := chatMessages(t, f, 101)
	require.NotEmpty(t, cards)
	assert.NotContains(t, cards[len(cards)-1].Text, "--template")
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	handleVisible(t, f.b, message(7401, 101, "/send_message_to"))
	cards = chatMessages(t, f, 101)
	assert.Contains(t, cards[len(cards)-1].Text, "--template")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
	require.NoError(t, err)
	handleVisible(t, f.b, message(7402, 101, "/send_message_to"))
	cards = chatMessages(t, f, 101)
	assert.NotContains(t, cards[len(cards)-1].Text, "--template")
}
