package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func seedScriptDomains(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at,titles) VALUES('script-dance',now()+interval '30 days','{"en":"Dance","ru":"Танцы"}');
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,invitation_target) VALUES
 ('script-dance','alice',1,'waitlist','leader','solo','bob',now(),0),('script-dance','bob',1,'waiting-for-couple','follower','couple','bob',now(),101);
 INSERT INTO core.massage_events(id) VALUES('sandbox-festival');
 INSERT INTO core.massage_parties(id,event_id,starts_at,ends_at,tables) VALUES('script-night','sandbox-festival',now()+interval '1 day',now()+interval '1 day 4 hours',2);
 INSERT INTO core.massage_specialists(event_id,owner,name,about,legacy_table_flag) VALUES('sandbox-festival','bob','Bob','{"en":"Professional","ru":"Специалист"}',true);
 INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at) SELECT 'sandbox-festival','bob',starts_at,ends_at FROM core.massage_parties WHERE id='script-night';
 INSERT INTO core.massage_bookings(id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub)
 SELECT 'own-massage','sandbox-festival',id,'alice','bob',1,1,starts_at,starts_at+interval '15 minutes',10,10 FROM core.massage_parties WHERE id='script-night';
 INSERT INTO core.massage_bookings(id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub)
 SELECT 'foreign-massage','sandbox-festival',id,'visitor','bob',8,1,starts_at+interval '2 hours',starts_at+interval '2 hours 15 minutes',10,10 FROM core.massage_parties WHERE id='script-night';`,
	)
	require.NoError(t, err)
}

func domainHTTP(t *testing.T, f *fixture, actor, path string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.b.API.Base+path, nil)
	require.NoError(t, err)
	if actor != "" {
		request.Header.Set("Authorization", "Bearer "+f.b.API.Signer.Token(actor))
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	require.NoError(t, err)
	return response.StatusCode, data
}

func TestScriptDomainOwnerReadsAndLocales(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			seedScriptDomains(t, f)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.users SET language=$1,can_book=false WHERE id='alice'`,
				language,
			)
			require.NoError(t, err)
			model := runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						require.Len(t, tools, 55)
						list := scriptCall(ctx, t, callback, "$list", "{}")
						assert.Contains(t, string(list), "history.read")
						assert.NotContains(t, string(list), "massage.timetable")
						assert.NotContains(t, string(list), "passes.admin")
						_, callErr := callback(
							ctx,
							scriptclient.ToolCall{
								Name:      "massage.bookings",
								Arguments: json.RawMessage(`{"view":"timetable"}`),
							},
						)
						require.Error(t, callErr)
						_, callErr = callback(
							ctx,
							scriptclient.ToolCall{
								Name:      "passes.get",
								Arguments: json.RawMessage(`{"event":"script-dance","owner":"bob"}`),
							},
						)
						require.Error(t, callErr)
						events := scriptCall(ctx, t, callback, "passes.events", "{}")
						assert.Contains(t, string(events), "Dance")
						assert.Contains(t, string(events), "Танцы")
						assert.Contains(t, string(events), `"titles_excerpt":true`)
						booking := scriptCall(ctx, t, callback, "passes.get", `{"event":"script-dance"}`)
						var own passbooking.Booking
						require.NoError(t, json.Unmarshal(booking, &own))
						assert.Equal(t, "alice", own.Owner)
						invitations := scriptCall(ctx, t, callback, "passes.invitations", `{"event":"script-dance"}`)
						assert.Contains(t, string(invitations), `"owner":"bob"`)
						parties := scriptCall(ctx, t, callback, "massage.parties", "{}")
						assert.Contains(t, string(parties), "script-night")
						slots := scriptCall(ctx, t, callback, "massage.slots", `{"party":"script-night","length":1}`)
						assert.Contains(t, string(slots), `"specialist":"bob"`)
						bookings := scriptCall(ctx, t, callback, "massage.bookings", "{}")
						assert.Contains(t, string(bookings), "own-massage")
						assert.NotContains(t, string(bookings), "foreign-massage")
						eventDetail := scriptCall(ctx, t, callback, "passes.event.read", `{"event":"script-dance"}`)
						providerDetail := scriptCall(ctx, t, callback, "massage.provider.read", `{"provider":"bob"}`)
						for _, data := range []json.RawMessage{eventDetail, providerDetail} {
							var chunk core.ReadChunk
							require.NoError(t, json.Unmarshal(data, &chunk))
							assert.False(t, chunk.More)
							assert.True(t, json.Valid([]byte(chunk.JSON)))
							assert.NotContains(t, chunk.JSON, "notify_bookings")
							assert.NotContains(t, chunk.JSON, "legacy_table_flag")
							assert.NotContains(t, chunk.JSON, `"work"`)
						}
						return json.RawMessage(`{"read":true}`), nil
					},
				),
			)
			require.Len(t, model.inputs, 2)
			for _, call := range model.inputs[1].Script.Runs[0].Calls {
				assert.Contains(t, string(call.Result), "payload_omitted")
			}
		})
	}
}

func readDomainDetail(t *testing.T, f *fixture, path string) string {
	t.Helper()
	cursor := ""
	var complete strings.Builder
	for range 300 {
		status, data := domainHTTP(t, f, "alice", path+"&cursor="+url.QueryEscape(cursor))
		require.Equal(t, http.StatusOK, status, string(data))
		require.LessOrEqual(t, len(data), core.ReadPageBytes)
		var chunk core.ReadChunk
		require.NoError(t, json.Unmarshal(data, &chunk))
		complete.WriteString(chunk.JSON)
		if !chunk.More {
			return complete.String()
		}
		cursor = chunk.NextCursor
	}
	t.Fatal("detail continuation did not terminate")
	return ""
}

func TestScriptDomainNavigationLargeCollectionsAndDetails(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	title := strings.Repeat("З🌍", 4000)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at,titles) SELECT 'large-'||i,now()+interval '20 days',jsonb_build_object('en',$1::text,'ru',$1::text) FROM generate_series(1,30) i`,
		title,
	)
	require.NoError(t, err)
	var total int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT sum(octet_length(titles::text)) FROM core.pass_events`).Scan(&total),
	)
	require.Greater(t, total, 1<<20)
	cursor := ""
	ids := map[string]bool{}
	for {
		status, data := domainHTTP(t, f, "alice", "/v1/passes/events/page?cursor="+url.QueryEscape(cursor))
		require.Equal(t, http.StatusOK, status, string(data))
		require.LessOrEqual(t, len(data), core.ReadPageBytes)
		var page core.ReadPage[passbooking.NavigationEvent]
		require.NoError(t, json.Unmarshal(data, &page))
		for _, event := range page.Items {
			require.False(t, ids[event.ID])
			ids[event.ID] = true
			assert.True(t, event.TitlesExcerpt)
		}
		if !page.More {
			break
		}
		cursor = page.NextCursor
	}
	require.Len(t, ids, 31)
	var full passbooking.Event
	require.NoError(
		t,
		json.Unmarshal([]byte(readDomainDetail(t, f, "/v1/passes/events/large-1/detail?unused=1")), &full),
	)
	assert.Equal(t, title, full.Titles["ru"])
	assert.Equal(t, title, full.Titles["en"])
	about := strings.Repeat("Provider🌍 ", 30000)
	for _, statement := range []string{
		`INSERT INTO core.massage_specialists(event_id,owner,name,about,legacy_table_flag) VALUES('sandbox-festival','visitor','Guest',jsonb_build_object('en',$1::text,'ru',$1::text),true)`,
		`UPDATE core.massage_specialists SET about=jsonb_build_object('en',$1::text,'ru',$1::text) WHERE owner='bob'`,
	} {
		_, err = f.db.Exec(t.Context(), statement, about)
		require.NoError(t, err)
	}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at) SELECT 'sandbox-festival','visitor',starts_at,ends_at FROM core.massage_parties WHERE id='script-night'`,
	)
	require.NoError(t, err)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT sum(octet_length(about::text)) FROM core.massage_specialists`).Scan(&total),
	)
	require.Greater(t, total, 1<<20)
	status, data := domainHTTP(
		t,
		f,
		"alice",
		"/v1/massage/slots/page?event=sandbox-festival&party=script-night&length=1",
	)
	require.Equal(t, http.StatusOK, status, string(data))
	require.LessOrEqual(t, len(data), core.ReadPageBytes)
	assert.Contains(t, string(data), "name_excerpt")
	assert.NotContains(t, string(data), "Provider🌍")
	var provider massage.PublicProvider
	require.NoError(
		t,
		json.Unmarshal(
			[]byte(readDomainDetail(t, f, "/v1/massage/providers/bob/detail?event=sandbox-festival")),
			&provider,
		),
	)
	assert.Equal(t, about, provider.About["ru"])
	assert.Equal(t, about, provider.About["en"])
}

func TestScriptDomainCursorIsolationAndRecovery(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	_, seedErr := f.db.Exec(
		t.Context(),
		`UPDATE core.massage_parties SET ends_at=starts_at+interval '12 hours' WHERE id='script-night'; UPDATE core.massage_work SET ends_at=starts_at+interval '12 hours' WHERE event_id='sandbox-festival';`,
	)
	require.NoError(t, seedErr)
	model := runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				first := scriptCall(ctx, t, callback, "massage.slots", `{"party":"script-night","length":1}`)
				var page core.ReadPage[massage.NavigationSlot]
				require.NoError(t, json.Unmarshal(first, &page))
				require.True(t, page.More)
				path := "/v1/massage/slots/page?event=sandbox-festival&party=script-night&length=1&cursor=" + url.QueryEscape(
					page.NextCursor,
				)
				status, _ := domainHTTP(t, f, "bob", path)
				assert.Equal(t, http.StatusBadRequest, status)
				status, _ = domainHTTP(t, f, "alice", strings.Replace(path, "length=1", "length=2", 1))
				assert.Equal(t, http.StatusBadRequest, status)
				status, _ = domainHTTP(t, f, "alice", strings.Replace(path, "event=sandbox-festival", "event=other", 1))
				assert.Equal(t, http.StatusBadRequest, status)
				_, err := f.db.Exec(ctx, `UPDATE core.massage_specialists SET name='Renamed' WHERE owner='bob'`)
				require.NoError(t, err)
				args, err := json.Marshal(
					map[string]any{"party": "script-night", "length": 1, "cursor": page.NextCursor},
				)
				require.NoError(t, err)
				stale := scriptCall(ctx, t, callback, "massage.slots", string(args))
				assert.JSONEq(t, `{"error":"stale","restart":true}`, string(stale))
				_, err = f.db.Exec(
					ctx,
					`UPDATE core.massage_specialists SET about=jsonb_build_object('en',repeat('x',1048577)) WHERE owner='bob'`,
				)
				require.NoError(t, err)
				limited := scriptCall(ctx, t, callback, "massage.provider.read", `{"provider":"bob"}`)
				assert.JSONEq(t, `{"error":"result_limit"}`, string(limited))
				return json.RawMessage(`{"recovery_seen":true}`), nil
			},
		),
	)
	require.Len(t, model.inputs, 2)
	calls := model.inputs[1].Script.Runs[0].Calls
	require.Len(t, calls, 3)
	assert.Equal(t, "stale", calls[1].Error)
	assert.Equal(t, "result_limit", calls[2].Error)
}

func TestDomainDetailCursorsBindOwnerAndResource(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedScriptDomains(t, f)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET titles=jsonb_build_object('ru',repeat('🌍',6000)) WHERE id='script-dance'`,
	)
	require.NoError(t, err)
	status, data := domainHTTP(t, f, "alice", "/v1/passes/events/script-dance/detail")
	require.Equal(t, http.StatusOK, status)
	var chunk core.ReadChunk
	require.NoError(t, json.Unmarshal(data, &chunk))
	require.True(t, chunk.More)
	path := "/v1/passes/events/script-dance/detail?cursor=" + url.QueryEscape(chunk.NextCursor)
	status, _ = domainHTTP(t, f, "bob", path)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = domainHTTP(t, f, "alice", strings.Replace(path, "/script-dance/", "/other/", 1))
	assert.Equal(t, http.StatusBadRequest, status)
	_, err = f.db.Exec(t.Context(), `UPDATE core.pass_events SET titles='{"en":"changed"}' WHERE id='script-dance'`)
	require.NoError(t, err)
	status, data = domainHTTP(t, f, "alice", path)
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, string(data), "read_stale")
	status, _ = domainHTTP(t, f, "", "/v1/passes/events/page")
	assert.Equal(t, http.StatusUnauthorized, status)
}
