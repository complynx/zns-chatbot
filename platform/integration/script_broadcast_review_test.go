package integration_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func broadcastScript(t *testing.T, f *fixture, update int64, code string, input any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: string(raw)}},
		{View: "workflow", Text: "Checked"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, 101, "Review the selected broadcast"))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.NotEmpty(t, runs)
	run := runs[len(runs)-1]
	require.Empty(t, run.Error)
	return run.Result
}

func TestBroadcastReviewSobekPaginationAndManualControls(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	service := adminmessage.Service{DB: f.db}
	request := adminmessage.Request{Content: adminmessage.Content{Text: strings.Repeat("complete content", 5)}}
	for index := range 21 {
		request.Destinations = append(request.Destinations, adminmessage.Destination{Chat: strconv.Itoa(1000 + index)})
	}
	draft, err := service.Preview(t.Context(), "alice", "review-pagination", request)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	const read = `let cursor="", text="", calls=0;
do { const chunk=await tools.broadcasts.review({id:input.id,offset:input.offset,cursor});
text+=chunk.json; cursor=chunk.next_cursor; calls++; } while(cursor);
const page=JSON.parse(text); return {total:page.total,more:page.more,count:page.items.length,last:page.items.at(-1),calls};`
	var first struct {
		Total int64                 `json:"total"`
		More  bool                  `json:"more"`
		Count int                   `json:"count"`
		Last  adminmessage.Delivery `json:"last"`
		Calls int                   `json:"calls"`
	}
	result := broadcastScript(t, f, 19100, read, map[string]any{"id": draft.ID, "offset": 0})
	require.NoError(t, json.Unmarshal(result, &first))
	assert.EqualValues(t, 21, first.Total)
	assert.Equal(t, 20, first.Count)
	assert.True(t, first.More)
	assert.Greater(t, first.Calls, 1, "page exceeds one JSON chunk")
	result = broadcastScript(t, f, 19101, read, map[string]any{"id": draft.ID, "offset": 20})
	require.NoError(t, json.Unmarshal(result, &first))
	assert.Equal(t, 1, first.Count)
	assert.False(t, first.More)
	assert.Equal(t, "1020", first.Last.Destination.Chat)
	assert.Equal(t, request.Content, first.Last.Content)
	result = broadcastScript(t, f, 19102,
		`return await tools.broadcasts.show({id:input.id,offset:20});`, map[string]any{"id": draft.ID})
	assert.JSONEq(t, fmt.Sprintf(`{"id":%d,"displayed":true,"manual_send_required":true}`, draft.ID), string(result))
	cards := chatMessages(t, f, 101)
	encoded, err := json.Marshal(cards)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), fmt.Sprintf("adminmsg:send:%d", draft.ID))
	assert.Contains(t, string(encoded), fmt.Sprintf("adminmsg:inspect:%d:20", draft.ID))
	assert.Contains(t, string(encoded), fmt.Sprintf("adminmsg:page:%d:0", draft.ID))
	var deliveries int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_message_deliveries`).Scan(&deliveries),
	)
	assert.Zero(t, deliveries)
	var receipt json.RawMessage
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=19102 AND kind='script_runs'`).
			Scan(&receipt),
	)
	assert.Contains(t, string(receipt), `"broadcast_review"`)
	assert.Contains(t, string(receipt), `"chat": 101`)
	assert.Contains(t, string(receipt), `"displayed": true`)
	// Replaying the Telegram update after revocation must not repeat the display.
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
	require.NoError(t, err)
	handle(t, f.b, message(19102, 101, "Review the selected broadcast"))
	assert.Len(t, chatMessages(t, f, 101), len(cards))
}

func TestBroadcastReviewSobekPreparationAndDeliveryResults(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			service := adminmessage.Service{DB: f.db}
			draft, err := service.Preview(t.Context(), "alice", "preparation", adminmessage.Request{
				Destinations: []adminmessage.Destination{
					{Chat: "202"},
				},
				Content: adminmessage.Content{Text: "Prepared text"},
			})
			require.NoError(t, err)
			// A persisted preparation awaiting its renderer, as after a process interruption.
			_, err = f.db.Exec(t.Context(), `UPDATE core.admin_messages SET state='preparing' WHERE id=$1`, draft.ID)
			require.NoError(t, err)
			f.b.Scripts = scopeVM{}
			input := map[string]any{"id": draft.ID}
			read := `return JSON.parse((await tools.broadcasts.review(input)).json);`
			var page adminmessage.Page
			require.NoError(t, json.Unmarshal(broadcastScript(t, f, 19400, read, input), &page))
			assert.Equal(t, "preparing", page.State)
			broadcastScript(t, f, 19401, `return await tools.broadcasts.show(input);`, input)
			page, err = service.Review(t.Context(), "alice", draft.ID, 0)
			require.NoError(t, err)
			assert.Equal(t, "draft", page.State)
			var deliveries int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_message_deliveries`).Scan(&deliveries),
			)
			assert.Zero(t, deliveries)
			cards := chatMessages(t, f, 101)
			var sendCard int64
			for _, card := range cards {
				for _, row := range card.Markup.Rows {
					for _, button := range row {
						if button.Data == fmt.Sprintf("adminmsg:send:%d", draft.ID) {
							sendCard = card.ID
						}
					}
				}
			}
			require.Positive(t, sendCard)
			handle(t, f.b, aliceCallback(19402, sendCard, fmt.Sprintf("adminmsg:send:%d", draft.ID)))
			delivery, found, err := service.Claim(t.Context())
			require.NoError(t, err)
			require.True(t, found)
			require.NoError(t, service.Complete(t.Context(), delivery.ID, delivery.Attempt, 9001, "", false))
			require.NoError(t, json.Unmarshal(broadcastScript(t, f, 19403, read, input), &page))
			assert.Equal(t, "queued", page.State)
			require.Len(t, page.Items, 1)
			assert.EqualValues(t, 9001, page.Items[0].TelegramMessageID)
			assert.EqualValues(t, 1, page.Items[0].Attempt)
			assert.Equal(t, "sent", page.Items[0].State)
		})
	}
}

func TestBroadcastReviewSobekCurrentAuthority(t *testing.T) {
	t.Parallel()
	f := setup(t)
	grant := func(enabled bool) {
		query := `DELETE FROM core.pass_booking_admins WHERE owner='alice'`
		if enabled {
			query = `INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING`
		}
		_, err := f.db.Exec(t.Context(), query)
		require.NoError(t, err)
	}
	const code = `const names=(await tools.$list()).map(t=>t.name);
const bound=typeof tools.broadcasts?.review==="function";
let help=false,called=false,show=false;
if(bound){try{await tools.broadcasts.review.$help();help=true;}catch(_){}
try{await tools.broadcasts.review({id:1});called=true;}catch(_){}
try{await tools.broadcasts.show({id:1});show=true;}catch(_){}}
return {listed:names.includes("broadcasts.review"),showListed:names.includes("broadcasts.show"),bound,help,called,show};`
	for index, step := range []struct {
		before func()
		want   string
	}{
		{nil, `{"listed":false,"showListed":false,"bound":false,"help":false,"called":false,"show":false}`},
		{func() { grant(true) }, `{"listed":false,"showListed":false,"bound":false,"help":false,"called":false,"show":false}`},
		{nil, `{"listed":true,"showListed":true,"bound":true,"help":true,"called":false,"show":false}`},
		{func() { grant(false) }, `{"listed":false,"showListed":false,"bound":true,"help":false,"called":false,"show":false}`},
	} {
		f.b.Scripts = scopeVM{before: step.before}
		result := broadcastScript(t, f, int64(19200+index), code, nil)
		assert.JSONEq(t, step.want, string(result))
	}
}

func TestBroadcastReviewSobekFullContentStaleAndOwnership(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice'),('bob')`)
	require.NoError(t, err)
	service := adminmessage.Service{DB: f.db}
	request := adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "202"}},
		Content:      adminmessage.Content{Text: strings.Repeat("Я", 4096)},
	}
	draft, err := service.Preview(t.Context(), "alice", "review-full", request)
	require.NoError(t, err)
	other, err := service.Preview(t.Context(), "bob", "review-other", request)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	const code = `const a=await tools.broadcasts.review({id:input.id});
const b=await tools.broadcasts.review({id:input.id,cursor:a.next_cursor});
const page=JSON.parse(a.json+b.json);
let denied=0;
for(const args of [{id:input.other},{id:input.id,owner:"bob"},{id:input.id,offset:1,cursor:a.next_cursor}]){
try{await tools.broadcasts.review(args);}catch(_){denied++;}}
for(const args of [{id:input.other},{id:input.id,chat:202},{id:input.id,cursor:""}]){
try{await tools.broadcasts.show(args);}catch(_){denied++;}}
return {complete:page.items[0].content.text==="Я".repeat(4096),denied,cursor:a.next_cursor,more:b.more};`
	var result struct {
		Complete bool   `json:"complete"`
		Denied   int    `json:"denied"`
		Cursor   string `json:"cursor"`
		More     bool   `json:"more"`
	}
	raw := broadcastScript(t, f, 19300, code, map[string]any{"id": draft.ID, "other": other.ID})
	require.NoError(t, json.Unmarshal(raw, &result))
	assert.True(t, result.Complete)
	assert.Equal(t, 6, result.Denied)
	assert.False(t, result.More)
	require.NotEmpty(t, result.Cursor)
	require.NoError(t, service.Enqueue(t.Context(), "alice", draft.ID))
	raw = broadcastScript(
		t,
		f,
		19301,
		`return await tools.broadcasts.review(input);`,
		map[string]any{"id": draft.ID, "cursor": result.Cursor},
	)
	assert.JSONEq(t, `{"error":"stale","restart":true}`, string(raw))
}
