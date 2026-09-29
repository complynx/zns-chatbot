package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestScriptRegistrationSelfMutationRetainsCommittedOutcome(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := passbooking.Service{DB: f.db}
	original, err := service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "before-agent", passbooking.Booking{}),
	)
	require.NoError(t, err)
	cancelled, err := service.Execute(t.Context(), "alice", bookingCommand("cancel", "before-agent-cancel", original))
	require.NoError(t, err)
	require.Positive(t, cancelled.Version)
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{
			Code:      `await tools.passes.registration.read({event:"dance",view:"home"}); return tools.passes.registration.solo({event:"dance"});`,
			InputJSON: "null",
		}},
		{View: "workflow", Text: "Registration complete"},
	}}
	f.b.Model = model
	input := message(79401, 101, "Register me again at dance")
	handle(t, f.b, input)
	committed, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, "assigned", committed.State)
	require.Greater(t, committed.Version, cancelled.Version)
	require.Len(t, model.inputs, 2, "a successful own mutation must reach the next model step")
	runs := model.inputs[1].Script.Runs
	require.NotEmpty(t, runs)
	result := runs[len(runs)-1]
	require.Empty(t, result.Error)
	var outcome struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &outcome))
	assert.True(t, outcome.Complete)
	assert.NotEmpty(t, outcome.ID)
	handle(t, f.b, input)
	replayed, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, committed, replayed)
	assert.Len(t, model.inputs, 2)
}

const registrationOldSecret = "registration-old-private-canary"

type registrationMutationTransport struct {
	before func() error
	after  func() error
	lose   bool
	hits   atomic.Int64
}

func (transport *registrationMutationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	mutation := request.Method == http.MethodPost && request.URL.Path == "/internal/derived/pass-actions"
	if mutation && transport.before != nil {
		if err := transport.before(); err != nil {
			return nil, err
		}
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil || !mutation || response.StatusCode != http.StatusOK {
		return response, err
	}
	transport.hits.Add(1)
	if transport.after != nil {
		if err = transport.after(); err != nil {
			_ = response.Body.Close()
			return nil, err
		}
	}
	if transport.lose {
		_ = response.Body.Close()
		return nil, io.ErrUnexpectedEOF
	}
	return response, nil
}

type registrationLeakingVM struct{ scopeVM }

func (vm registrationLeakingVM) Execute(ctx context.Context, request scriptclient.Request,
	tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
	_, _ = vm.scopeVM.Execute(ctx, request, tools, callback)
	// A worker response can contain old reads, transformed text, or collected logs.
	return json.RawMessage(`{"old":"registration-old-private-canary","logs":["registration-old-private-canary"]}`), nil
}

func prepareRegistrationSelfMutation(t *testing.T) (*fixture, passbooking.Booking) {
	t.Helper()
	f := passMenuFixture(t)
	service := passbooking.Service{DB: f.db}
	original, err := service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "before-agent", passbooking.Booking{}),
	)
	require.NoError(t, err)
	cancelled, err := service.Execute(t.Context(), "alice", bookingCommand("cancel", "before-agent-cancel", original))
	require.NoError(t, err)
	return f, cancelled
}

func TestScriptRegistrationSelfMutationDiscardsVMData(t *testing.T) {
	t.Parallel()
	for _, leakedWorker := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "old_read_return", true: "worker_result_and_logs"}[leakedWorker],
			func(t *testing.T) {
				t.Parallel()
				f, cancelled := prepareRegistrationSelfMutation(t)
				_, err := f.db.Exec(
					t.Context(),
					`UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice' AND event_id='dance'`,
					registrationOldSecret,
				)
				require.NoError(t, err)
				transport := &registrationMutationTransport{before: func() error {
					_, clearErr := f.db.Exec(
						t.Context(),
						`UPDATE core.pass_bookings SET comment='' WHERE owner='alice' AND event_id='dance'`,
					)
					return clearErr
				}}
				f.b.Host.HTTP = &http.Client{Transport: transport}
				f.b.Scripts = scopeVM{}
				if leakedWorker {
					f.b.Scripts = registrationLeakingVM{}
				}
				code := `const old = await tools.passes.registration.read({event:"dance",view:"home"});
if(old.booking.comment !== "registration-old-private-canary") throw new Error("missing test source");
try { await tools.passes.registration.solo({event:"dance"}); } catch (_) {}
await tools.passes.registration.cancel({event:"dance"});
return old;`
				model := &knowledgeModel{plans: []agent.Plan{
					{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
					{View: "workflow", Text: "Registration complete"},
				}}
				f.b.Model = model
				update := message(79402, 101, "Register me again")
				handle(t, f.b, update)
				require.EqualValues(
					t,
					1,
					transport.hits.Load(),
					"the canceled worker must not execute the following mutation",
				)
				committed, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "alice", "dance")
				require.NoError(t, err)
				require.Equal(t, "assigned", committed.State)
				require.Equal(t, cancelled.Version+1, committed.Version)
				require.Len(t, model.inputs, 2)
				projected, err := json.Marshal(model.inputs[1].Script)
				require.NoError(t, err)
				assert.NotContains(t, string(projected), registrationOldSecret)
				assert.NotContains(t, string(projected), `"state":"cancelled"`)
				run := model.inputs[1].Script.Runs[0]
				require.Empty(t, run.Error)
				var status struct {
					Complete bool `json:"complete"`
				}
				require.NoError(t, json.Unmarshal(run.Result, &status))
				assert.True(t, status.Complete)
				var records []agenthost.ScriptRecord
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=79402 AND kind='script_runs'`).
						Scan(&records),
				)
				require.Len(t, records, 1)
				require.Len(t, records[0].Calls, 2)
				assert.Empty(t, records[0].Request.Code)
				assert.Empty(t, records[0].Calls[0].Outcome.Result)
				assert.Empty(t, records[0].ReadAuthorities)
				admitted := records[0].Calls[1]
				require.NotNil(t, admitted.Pass.Witness)
				assert.True(t, admitted.Pass.Witness.Valid("alice"))
				assert.Equal(t, cancelled.Version, admitted.Pass.Command.Version)
				require.NotNil(t, admitted.Source)
				assert.Contains(
					t,
					string(mustJSON(t, admitted.Source)),
					`"version":2`,
					"original admission is not rebased",
				)
				handle(t, f.b, update)
				assert.EqualValues(t, 1, transport.hits.Load())
				assert.Len(t, model.inputs, 2)
			},
		)
	}
}

func TestScriptRegistrationSelfMutationRejectsInvalidation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"external_version", "external_recreation", "permission", "history", "lost_response"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, cancelled := prepareRegistrationSelfMutation(t)
			_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
			require.NoError(t, err)
			service := passbooking.Service{DB: f.db}
			transport := &registrationMutationTransport{lose: change == "lost_response"}
			transport.after = func() error {
				committed, getErr := service.Get(t.Context(), "alice", "dance")
				require.NoError(t, getErr)
				require.Equal(t, cancelled.Version+1, committed.Version)
				require.Equal(t, "assigned", committed.State)
				switch change {
				case "external_version":
					_, getErr = service.Execute(
						t.Context(),
						"alice",
						bookingCommand("cancel", "external-after-commit", committed),
					)
				case "external_recreation":
					_, getErr = f.db.Exec(t.Context(), `WITH previous AS (
DELETE FROM core.pass_bookings WHERE owner='alice' AND event_id='dance' RETURNING *)
INSERT INTO core.pass_bookings SELECT (jsonb_populate_record(NULL::core.pass_bookings,
to_jsonb(previous)||jsonb_build_object('created_at',clock_timestamp()))).* FROM previous`)
					if getErr == nil {
						recreated, readErr := service.Get(t.Context(), "alice", "dance")
						require.NoError(t, readErr)
						require.Equal(t, committed.Version, recreated.Version)
						require.False(t, committed.CreatedAt.Equal(recreated.CreatedAt))
					}
				case "permission":
					_, getErr = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
				case "history":
					var id int64
					getErr = f.db.QueryRow(t.Context(), `SELECT min(id) FROM core.conversation_events WHERE owner='alice'`).
						Scan(&id)
					if getErr == nil {
						getErr = (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", id)
					}
				}
				return getErr
			}
			f.b.Host.HTTP = &http.Client{Transport: transport}
			f.b.Scripts = scopeVM{}
			model := &knowledgeModel{plans: []agent.Plan{
				{View: "workflow", ScriptAction: &agent.ScriptProposal{
					Code:      `await tools.passes.tiers({event:"dance"}); await tools.passes.registration.read({event:"dance",view:"home"}); return tools.passes.registration.solo({event:"dance"});`,
					InputJSON: "null",
				}},
				{View: "workflow", Text: "Must not reach this model call"},
			}}
			f.b.Model = model
			_ = f.b.Handle(t.Context(), message(79403, 101, "Register me again"))
			require.EqualValues(t, 1, transport.hits.Load(), "control must happen after one committed mutation")
			assert.Len(
				t,
				model.inputs,
				1,
				"external changes or an unproven response must not use the own-transition path",
			)
		})
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	result, err := json.Marshal(value)
	require.NoError(t, err)
	return result
}

func TestScriptRegistrationSelfMutationOmitsEarlierRun(t *testing.T) {
	t.Parallel()
	f, cancelled := prepareRegistrationSelfMutation(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice' AND event_id='dance'`,
		registrationOldSecret,
	)
	require.NoError(t, err)
	transport := &registrationMutationTransport{before: func() error {
		_, clearErr := f.db.Exec(
			t.Context(),
			`UPDATE core.pass_bookings SET comment='' WHERE owner='alice' AND event_id='dance'`,
		)
		return clearErr
	}}
	f.b.Host.HTTP = &http.Client{Transport: transport}
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{
			Code: `return tools.passes.registration.read({event:"dance",view:"home"});`, InputJSON: "null",
		}},
		{View: "workflow", ScriptAction: &agent.ScriptProposal{
			Code: `return tools.passes.registration.solo({event:"dance"});`, InputJSON: "null",
		}},
		{View: "workflow", Text: "Registration complete"},
	}}
	f.b.Model = model
	update := message(79404, 101, "Read my registration and register again")
	handle(t, f.b, update)
	require.Len(t, model.inputs, 3)
	require.Contains(t, string(model.inputs[1].Script.Runs[0].Result), registrationOldSecret)
	require.Len(t, model.inputs[2].Script.Runs, 2)

	var omitted struct {
		Omitted bool   `json:"omitted"`
		Reason  string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(model.inputs[2].Script.Runs[0].Result, &omitted))
	assert.True(t, omitted.Omitted)
	assert.Equal(t, "source_changed", omitted.Reason)
	assert.False(t, model.inputs[2].Script.Runs[0].PassRedacted)
	oldAuthority := interaction.PlanAuthority{
		Reads:           []interaction.PassContextDependency{},
		ReadAuthorities: readsource.CloneAuthorities(model.inputs[1].Script.ReadAuthorities),
		Scripts:         true,
	}
	require.NotEmpty(t, oldAuthority.ReadAuthorities)
	policy := agenthost.PlanAuthorization{Sources: registrationSavedPlanSources{
		liveLedgerAuthority: liveLedgerAuthority{fixture: f},
	}}
	changed, err := policy.Changed(t.Context(), "alice", 79404, &oldAuthority)
	require.NoError(t, err)
	assert.True(t, changed, "saved old read or choice authority must not become valid after output omission")
	require.NoError(t, f.b.Host.CheckReadAuthorities(t.Context(), "alice", model.inputs[2].Script.ReadAuthorities))

	assert.NotContains(t, string(mustJSON(t, model.inputs[2].Script)), registrationOldSecret)
	var status struct {
		Complete bool `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(model.inputs[2].Script.Runs[1].Result, &status))
	assert.True(t, status.Complete)
	booking, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, cancelled.Version+1, booking.Version)
	assert.Equal(t, "assigned", booking.State)
	handle(t, f.b, update)
	assert.EqualValues(t, 1, transport.hits.Load())
	assert.Len(t, model.inputs, 3)
}

type registrationSavedPlanSources struct {
	liveLedgerAuthority
}

func (policy registrationSavedPlanSources) SourcesChanged(
	ctx context.Context,
	owner string,
	refs []readsource.Authority,
) (bool, error) {
	return agenthost.ReadAuthoritiesChanged(ctx, policy.fixture.b.Host, owner, refs)
}

func (policy registrationSavedPlanSources) RegistrationContextChanged(
	ctx context.Context,
	owner string,
	dependency interaction.PassContextDependency,
) (bool, error) {
	refs, err := agenthost.PassContextReadAuthorities([]interaction.PassContextDependency{dependency})
	if err != nil {
		return false, err
	}
	return policy.SourcesChanged(ctx, owner, refs)
}
