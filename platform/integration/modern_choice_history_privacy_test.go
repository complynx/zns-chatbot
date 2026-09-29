package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const choiceHistoryCanary = "iris_private_draft_context"

func privateModernDraft(t *testing.T, f *fixture, update int64, orderID string) (string, int64) {
	t.Helper()
	archive := conversation.Service{DB: f.db}
	require.NoError(
		t,
		archive.AppendOriginal(t.Context(), "alice", "private-choice-source", "user", choiceHistoryCanary),
	)
	var eventID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='private-choice-source'`).
			Scan(&eventID),
	)
	prefix := ""
	begin := `tools.orders.choice({operation:"begin",empty:true})`
	if orderID != "" {
		prefix = fmt.Sprintf(`tools.orders.inspect({order_id:%q});`, orderID)
		begin = fmt.Sprintf(`tools.orders.choice({operation:"begin",order_id:%q})`, orderID)
	}
	result := runModernContinuation(
		t,
		f,
		update,
		identity.AliceTelegramID,
		"Prepare draft "+orderID,
		fmt.Sprintf(
			`%s const h=tools.history.read({event_id:%d});let d=%s;d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:h.text});return {ref:d.choice_ref};`,
			prefix,
			eventID,
			begin,
		),
	)
	var draft struct {
		Ref string `json:"ref"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	require.NotEmpty(t, draft.Ref)
	return draft.Ref, eventID
}

func backlogPrivateDraft(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 SELECT 'alice',g,'script_runs','[{"history_generation":0}]'::jsonb FROM generate_series(1000,1999) g`)
	require.NoError(t, err)
}

func TestModernChoiceDeletedGenerationRejectsEveryUse(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"summary", "choice", "catalog", "quote", "patch", "create", "edit"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			orderID := ""
			if operation == "edit" {
				order, err := (orders.Service{DB: f.db}).Execute(
					t.Context(),
					"alice",
					orders.Command{
						EventID: "sandbox-festival",
						Name:    "create",
						Origin:  "manual",
						Key:     "existing-business-order",
						Choice:  &orders.ChoiceInput{Customer: "committed business customer"},
					},
				)
				require.NoError(t, err)
				orderID = order.ID
			}
			ref, eventID := privateModernDraft(t, f, 92001, orderID)
			backlogPrivateDraft(t, f)
			require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", eventID))
			var expression string
			switch operation {
			case "summary", "choice", "catalog":
				expression = fmt.Sprintf(
					`tools.orders.choice({operation:"read",part:%q,choice_ref:%q});`,
					operation,
					ref,
				)
			case "quote":
				expression = fmt.Sprintf(`tools.orders.quote({choice_ref:%q});`, ref)
			case "patch":
				expression = fmt.Sprintf(
					`tools.orders.choice({operation:"patch",choice_ref:%q,customer_first_name:"fresh"});`,
					ref,
				)
			case "create":
				expression = fmt.Sprintf(`tools.orders.update({name:"create",choice_ref:%q});`, ref)
			case "edit":
				expression = fmt.Sprintf(
					`tools.orders.inspect({order_id:%q});tools.orders.update({name:"edit",order_id:%q,choice_ref:%q});`,
					orderID,
					orderID,
					ref,
				)
			}
			assertModernBoundaryDenied(t, f, 92002, identity.AliceTelegramID, expression)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE choice->>'customer'=$1`, choiceHistoryCanary).
					Scan(&count),
			)
			assert.Zero(t, count)
			if orderID != "" {
				current, err := f.b.API.Order(t.Context(), "alice", "sandbox-festival", orderID)
				require.NoError(t, err)
				assert.Equal(t, "committed business customer", current.Choice.Customer)
			}
		})
	}
}

func TestModernChoiceGenerationLegacyAndParentFailClosed(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing_draft", "missing_parent", "redacted_parent", "changed_parent"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ref := newModernBoundaryDraft(t, f, 92101)
			var query string
			switch mode {
			case "missing_draft":
				query = `UPDATE bot.interactions SET content=content#-'{0,calls,0,modern_choice,history_generation}' WHERE update_id=92101 AND kind='script_runs'`
			case "missing_parent":
				query = `UPDATE bot.interactions SET content=content#-'{0,history_generation}' WHERE update_id=92101 AND kind='script_runs'`
			case "redacted_parent":
				query = `UPDATE bot.interactions SET content=jsonb_set(content,'{0,history_redacted}','true') WHERE update_id=92101 AND kind='script_runs'`
			case "changed_parent":
				query = `UPDATE bot.interactions SET content=jsonb_set(content,'{0,history_generation}','1') WHERE update_id=92101 AND kind='script_runs'`
			}
			_, err := f.db.Exec(t.Context(), query)
			require.NoError(t, err)
			assertModernBoundaryDenied(
				t,
				f,
				92102,
				identity.AliceTelegramID,
				fmt.Sprintf(`tools.orders.choice({operation:"read",part:"choice",choice_ref:%q});`, ref),
			)
		})
	}
}

func TestModernChoiceDeletionAtCommitBoundary(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ref, eventID := privateModernDraft(t, f, 92201, "")
	called := false
	f.b.API.HTTP = &http.Client{Transport: qaHistoryDeleteTransport{before: func(r *http.Request) error {
		if r.URL.Path != "/v1/order-actions" && r.URL.Path != "/internal/derived/order-actions" {
			return nil
		}
		body, err := r.GetBody()
		if err != nil {
			return err
		}
		defer body.Close()
		payload, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		commandJSON, err := restartOrderCommand(r.URL.Path, payload)
		if err != nil {
			return err
		}
		var command orders.Command
		if err = json.Unmarshal(commandJSON, &command); err != nil {
			return err
		}
		assert.NotNil(t, command.HistoryGeneration, "expected generation is carried to Core")
		called = true
		return (conversation.Service{DB: f.db}).DeleteContent(r.Context(), "alice", eventID)
	}}}
	f.b.Host.HTTP = f.b.API.HTTP
	f.b.Scripts = scopeVM{}
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{
			View: agent.KnowledgeView,
			ScriptAction: &agent.ScriptProposal{
				Code:      fmt.Sprintf(`return tools.orders.update({name:"create",choice_ref:%q});`, ref),
				InputJSON: "null",
			},
		},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	err := f.b.Handle(t.Context(), message(92202, identity.AliceTelegramID, "Commit the prepared draft"))
	require.Error(t, err)
	assert.True(t, called)
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders`).Scan(&count))
	assert.Zero(t, count)
}

func TestModernChoiceCurrentGenerationAndCommittedOrderSurvive(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ref, eventID := privateModernDraft(t, f, 92301, "")
	result := runModernContinuation(
		t,
		f,
		92302,
		identity.AliceTelegramID,
		"Commit current draft",
		fmt.Sprintf(`return tools.orders.update({name:"create",choice_ref:%q});`, ref),
	)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(result, &created))
	require.NotEmpty(t, created.ID)
	require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", eventID))
	read := runModernContinuation(
		t,
		f,
		92303,
		identity.AliceTelegramID,
		"Inspect committed order "+created.ID,
		fmt.Sprintf(`return tools.orders.inspect({order_id:%q});`, created.ID),
	)
	assert.Contains(t, string(read), choiceHistoryCanary, "current ACL can read committed business state")
	fresh := newModernBoundaryDraft(t, f, 92304)
	result = runModernContinuation(
		t,
		f,
		92305,
		identity.AliceTelegramID,
		"Commit fresh draft",
		fmt.Sprintf(`return tools.orders.update({name:"create",choice_ref:%q});`, fresh),
	)
	assert.Contains(t, string(result), `"id"`)
	assert.NotContains(t, string(result), `"error"`)
	_, err := f.b.API.Order(t.Context(), "bob", "sandbox-festival", created.ID)
	require.Error(t, err)
}
