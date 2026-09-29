package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func runMassageScript(t *testing.T, f *fixture, actor, update int64, code string) json.RawMessage {
	t.Helper()
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: "workflow", Text: "Done"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, actor, "Perform the explicitly requested massage changes"))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.Len(t, runs, 1)
	require.Empty(t, runs[0].Error)
	return runs[0].Result
}

func TestScriptMassageBookingCancellationAndReplay(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			seedScriptDomains(t, f)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			result := runMassageScript(t, f, identity.AliceTelegramID, 9951, `
const page = tools.massage.slots({event:"sandbox-festival",party:"script-night",length:1});
const slot = page.items[0];
const booked = tools.massage.book({event:"sandbox-festival",party:"script-night",specialist:slot.specialist,start:slot.start,length:1});
const cancelled = tools.massage.cancel({event:"sandbox-festival",booking:booked.id});
return {same:booked.id===cancelled.id,version:cancelled.version,cancelled:!!cancelled.cancelled_at};`)
			assert.JSONEq(t, `{"same":true,"version":2,"cancelled":true}`, string(result))
			handle(t, f.b, message(9951, identity.AliceTelegramID, "Perform the explicitly requested massage changes"))
			var operations, bookings int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_operations WHERE actor='alice'`).
					Scan(&operations),
			)
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_bookings WHERE owner='alice'`).
					Scan(&bookings),
			)
			assert.Equal(t, 2, operations)
			assert.Equal(t, 2, bookings)
			var state string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state->>'view' FROM bot.massage_views WHERE owner='alice' AND revision=9951`).
					Scan(&state),
			)
			assert.Equal(t, "mine", state)
			var boundStart, boundKey string
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
content->0->'calls'->1->'massage'->'command'->>'expected_start',
content->0->'calls'->1->'massage'->'command'->>'key'
FROM bot.interactions WHERE owner='alice' AND update_id=9951 AND kind='script_runs'`).Scan(&boundStart, &boundKey))
			assert.NotEmpty(t, boundStart)
			assert.Equal(t, "tg-script-9951-0-1", boundKey)
		})
	}
}

func TestScriptMassageAuthorityAndRequiredFields(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	result := runMassageScript(t, f, identity.AliceTelegramID, 9952, `
let denied=0;
for (const args of [{event:"sandbox-festival",booking:"foreign-massage"},
{event:"sandbox-festival",booking:"own-massage",version:1},
{event:"sandbox-festival",booking:"own-massage",owner:"bob"}]) {
 try {tools.massage.cancel(args);} catch (_) {denied++;}
}
return {denied,hidden:typeof tools.massage.practitioner==="undefined"};`)
	assert.JSONEq(t, `{"denied":3,"hidden":true}`, string(result))
	var operations int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_operations`).Scan(&operations))
	assert.Zero(t, operations)
}

func TestScriptMassagePractitionerPreferencesAndInstant(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.massage_parties SET starts_at=date_trunc('minute',now())-interval '30 minutes',ends_at=now()+interval '2 hours' WHERE id='script-night';
UPDATE core.massage_work SET starts_at=(SELECT starts_at FROM core.massage_parties WHERE id='script-night'), ends_at=(SELECT ends_at FROM core.massage_parties WHERE id='script-night')`,
	)
	require.NoError(t, err)
	result := runMassageScript(t, f, identity.BobTelegramID, 9953, `
let omitted=false; try {tools.massage.practitioner.configure({event:"sandbox-festival",bookings:false});} catch (_) {omitted=true;}
const prefs=tools.massage.practitioner.configure({event:"sandbox-festival",bookings:false,next:true});
const booking=tools.massage.practitioner.instant({event:"sandbox-festival",party:"script-night",length:1});
return {omitted,prefs,self:booking.owner===booking.specialist,instant:booking.instant};`)
	assert.JSONEq(
		t,
		`{"omitted":true,"prefs":{"bookings":false,"next":true},"self":true,"instant":true}`,
		string(result),
	)
}

func TestMassageBoundStartRejectsChangedParty(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	available, err := f.b.API.MassageSlots(t.Context(), "alice", "sandbox-festival", "script-night", 1)
	require.NoError(t, err)
	require.NotEmpty(t, available.Slots)
	slot := available.Slots[0]
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.massage_parties SET starts_at=starts_at+interval '15 minutes' WHERE id='script-night'`,
	)
	require.NoError(t, err)
	_, err = f.b.API.ExecuteMassage(
		t.Context(),
		"alice",
		massage.Command{Key: "bound-start", Action: "book", Event: "sandbox-festival",
			Party: "script-night", Specialist: slot.Specialist, Slot: slot.Slot, Length: 1, ExpectedStart: &slot.Start},
	)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, "stale_slot", problem.Code)
	var operations int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_operations`).Scan(&operations))
	assert.Zero(t, operations)
}
