package integration_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type unavailablePassAssignment struct{}

func (unavailablePassAssignment) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/internal/derived/pass-assignments" {
		return nil, errors.New("synthetic assignment unavailable before execution")
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestPassOperationPublicSingleTargetSelection(t *testing.T) {
	t.Parallel()
	passOperationSelection(t, false)
}

func TestPassOperationCombinedSelectionRetiresResult(t *testing.T) {
	t.Parallel()
	passOperationSelection(t, true)
}

func passOperationSelection(t *testing.T, combined bool) {
	t.Helper()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Synthetic Name'`)
	require.NoError(t, err)
	f.b.Host.HTTP = &http.Client{Transport: unavailablePassAssignment{}}
	for index, target := range []int64{101, 202} {
		result := runPassVM(t, f, 66000+int64(index), 202,
			fmt.Sprintf("Assign pass for %d using profile", target), fmt.Sprintf(`
const target = await tools.passes.admin.target({event:"dance",target:"%d"});
return tools.passes.admin.assign({event:"dance",target:target.admin_target.booking.owner,
assignment:{create:true,from_profile:true,total_price:150}});`, target))
		require.Empty(t, result.Error)
		var admitted struct {
			ID          string `json:"operation_id"`
			Interrupted bool   `json:"interrupted"`
		}
		require.NoError(t, json.Unmarshal(result.Result, &admitted))
		require.NotEmpty(t, admitted.ID)
		require.True(t, admitted.Interrupted)
	}
	// Equal fixture timestamps ensure selection cannot rely on time or operation kind.
	_, err = f.db.Exec(t.Context(), `UPDATE bot.interactions SET created_at='2030-01-01T00:00:00Z'
WHERE owner='bob' AND kind='script_runs' AND update_id IN (66000,66001)`)
	require.NoError(t, err)
	f.b.Host.HTTP = f.b.API.HTTP
	selection := `
const operations = await tools.passes.operations({});
const assignments = operations.filter(o => o.tool === "passes.admin.assign");
if (assignments.length !== 2 || assignments[0].admitted_at !== assignments[1].admitted_at)
 throw new Error("same-kind same-time fixture unavailable");
const selected = assignments.find(o => o.context && o.context.target === "alice");
if (!selected) throw new Error("target not discoverable");
return selected;`
	if combined {
		selection = strings.Replace(selection, "return selected;",
			"return {selected, resumed: await tools.passes.resume({operation_id:selected.operation_id})};", 1)
	}
	if combined {
		assertCombinedPassRetirement(t, f, selection)
	} else {
		result := runPassVM(t, f, 66002, 202, "Select only the prior assignment for alice", selection)
		require.Empty(t, result.Error)
		var selected interaction.RegistrationOperationSummary
		require.NoError(t, json.Unmarshal(result.Result, &selected))
		require.NotNil(t, selected.Context)
		require.Equal(t, "alice", selected.Context.Target)
		resumed := runPassVM(t, f, 66003, 202, "Resume that selected assignment only",
			fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, selected.ID))
		require.Empty(t, resumed.Error)
		status := runPassVM(t, f, 66004, 202, "Read the selected operation receipt",
			fmt.Sprintf(`return tools.passes.operations({operation_id:%q});`, selected.ID))
		require.Empty(t, status.Error)
		var receipts []interaction.RegistrationOperationSummary
		require.NoError(t, json.Unmarshal(status.Result, &receipts))
		require.Len(t, receipts, 1)
		require.Equal(t, selected.ID, receipts[0].ID)
		require.Equal(t, "committed", receipts[0].Status)
	}
	service := passbooking.Service{DB: f.db}
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, "assigned", alice.State)
	require.Zero(t, bob.Version, "unselected operation must not execute")
}

func assertCombinedPassRetirement(t *testing.T, f *fixture, code string) {
	t.Helper()
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: "workflow", Text: "Checked"},
	}}
	f.b.Model = model
	handle(t, f.b, message(66002, 202, "Select and resume only the prior assignment for alice"))
	require.Len(t, model.inputs, 1, "retired source must stop model continuation")
	for _, displayed := range chatMessages(t, f, 202) {
		t.Logf("retirement visible text: %q", displayed.Text)
	}
	var body []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=66002 AND kind='script_runs'`).Scan(&body))
	var runs []agenthost.ScriptRecord
	require.NoError(t, json.Unmarshal(body, &runs))
	require.Len(t, runs, 1)
	require.True(t, runs[0].PassRedacted)
	require.Equal(t, "interrupted", runs[0].Run.Error)
	require.Len(t, runs[0].Calls, 2)
	executed := runs[0].Calls[1].Pass
	require.NotNil(t, executed)
	require.NotNil(t, executed.Witness)
	ledger := agenthost.ScriptStore{DB: f.db}
	originals, err := ledger.ReadRegistrationOperations(t.Context(), "bob", executed.ID)
	require.NoError(t, err)
	require.Len(t, originals, 1)
	require.Equal(t, originals[0].Witness, executed.Witness)
	var receipts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations
 WHERE actor='bob' AND event_id=$1 AND key_hash=$2`, executed.Witness.Event, fmt.Sprintf("%x", sha256.Sum256([]byte(executed.Witness.Key)))).Scan(&receipts))
	require.Equal(t, 1, receipts, "interruption does not undo the committed canonical receipt")
}

func TestPassOperationLongHistoryTypedRead(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	const historySize = 1000
	ids := make([]string, historySize)
	for index := range ids {
		ids[index] = rand.Text()
	}
	// Synthetic ledger admissions contain no receipt, outcome, source text or witness.
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content,created_at)
SELECT 'bob', 67000+ordinality, 'script_runs',
jsonb_build_array(jsonb_build_object('calls',jsonb_build_array(jsonb_build_object('pass',
jsonb_build_object('id',id,'name','passes.registration.show','menu',jsonb_build_object('event','dance')))))),
'2030-01-01T00:00:00Z'::timestamptz+ordinality*interval '1 second'
FROM unnest($1::text[]) WITH ORDINALITY AS entries(id,ordinality)`, ids)
	require.NoError(t, err)
	coordinator := interaction.RegistrationOperations{
		Ledger: agenthost.ScriptStore{DB: f.db}, Domain: f.b.Host,
	}
	recent, err := coordinator.Read(t.Context(), "bob", derivedmutation.PassOperationQuery{})
	require.NoError(t, err)
	require.Len(t, recent.Summaries, 20)
	require.Equal(t, ids[historySize-1], recent.Summaries[0].ID)
	require.Equal(t, ids[historySize-20], recent.Summaries[19].ID)
	oldest, err := coordinator.Read(t.Context(), "bob", derivedmutation.PassOperationQuery{ID: ids[0]})
	require.NoError(t, err)
	require.Len(t, oldest.Summaries, 1)
	require.Equal(t, ids[0], oldest.Summaries[0].ID)
	require.Equal(t, "unknown", oldest.Summaries[0].Status)
	require.Equal(t, "unavailable", oldest.Summaries[0].Continuation)
	require.NotEmpty(t, oldest.ReadAuthorities)
	_, err = coordinator.Read(t.Context(), "alice", derivedmutation.PassOperationQuery{ID: ids[0]})
	requireCode(t, err, "pass_operation_unavailable")
}
