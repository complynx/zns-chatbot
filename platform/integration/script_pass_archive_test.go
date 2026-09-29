package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func archivedPassFixture(t *testing.T) *fixture {
	t.Helper()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `
INSERT INTO core.pass_events(id,finishes_at,titles) VALUES('archive', '2025-12-01', '{"en":"Past dance","ru":"Прошлые танцы"}');
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
VALUES('archive','alice',1,'paid','leader','guest','bob','2025-09-01','2025-09-02',0);
INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,owner,source_kind,source_record_sha256,target_id,source_record)
VALUES(repeat('a',64),77,'archive','alice','embedded',repeat('b',64),'archive:alice','{}');
INSERT INTO core.legacy_pass_payment_metadata(event_id,owner,assigned_at,source_key,proof_reference)
VALUES('archive','alice','2025-09-02',repeat('a',64),'free_pass');
UPDATE core.users SET can_book=false WHERE id='alice';`)
	require.NoError(t, err)
	return f
}

func TestScriptPassArchivedDiscoveryPagesBindOwner(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(t.Context(), `
INSERT INTO core.pass_events(id,finishes_at,titles)
SELECT 'history-'||n,'2025-12-01','{"en":"History","ru":"История"}' FROM generate_series(1,45) n;
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
SELECT 'history-'||n,'alice',1,'cancelled','leader','solo','bob','2025-09-01' FROM generate_series(1,45) n;`)
	require.NoError(t, err)
	page := runPassVM(
		t,
		f,
		19820,
		101,
		"List my historical registrations",
		`const p=await tools.passes.events({}); return {ids:p.items.map(e=>e.id),next:p.next_cursor};`,
	)
	require.Empty(t, page.Error)
	var first struct {
		IDs  []string `json:"ids"`
		Next string   `json:"next"`
	}
	require.NoError(t, json.Unmarshal(page.Result, &first))
	require.NotEmpty(t, first.Next)
	foreign := runPassVM(
		t,
		f,
		19821,
		202,
		"Next page",
		fmt.Sprintf(`return tools.passes.events({cursor:%q});`, first.Next),
	)
	assert.NotEmpty(t, foreign.Error)
	rest := runPassVM(
		t,
		f,
		19822,
		101,
		"Continue my historical registrations",
		fmt.Sprintf(
			`let c=%q; const ids=[]; while(c){ const p=await tools.passes.events({cursor:c}); ids.push(...p.items.map(e=>e.id)); c=p.next_cursor; } return ids;`,
			first.Next,
		),
	)
	require.Empty(t, rest.Error)
	var remaining []string
	require.NoError(t, json.Unmarshal(rest.Result, &remaining))
	all := first.IDs
	all = append(all, remaining...)
	assert.Len(t, all, 47)
	seen := map[string]bool{}
	for _, id := range all {
		assert.False(t, seen[id], id)
		seen[id] = true
	}
	assert.True(t, seen["archive"])
	assert.True(t, seen["dance"])
	active, err := (passbooking.Service{DB: f.db}).Events(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "dance", active[0].ID)
}

func TestScriptPassArchivedOwnerReadAndShow(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	read := runPassVM(
		t,
		f,
		19800,
		101,
		"Read my archive payment",
		`return tools.passes.registration.read({event:"archive",view:"payment"});`,
	)
	require.Empty(t, read.Error)
	var result agent.RegistrationReadResult
	require.NoError(t, json.Unmarshal(read.Result, &result))
	require.Empty(t, result.Error)
	require.NotNil(t, result.Payment)
	assert.Equal(t, "free", result.Payment.Kind)
	assert.Equal(t, "accepted", result.Payment.Decision)
	assert.Nil(t, result.Payment.ReceivedAt)
	assert.Nil(t, result.Payment.ReviewedAt)
	assert.Empty(t, result.Payment.Attempt)
	assert.True(t, result.Historical)
	show := runPassVM(
		t,
		f,
		19801,
		101,
		"Show my archive payment",
		`return tools.passes.registration.show({event:"archive",view:"payment"});`,
	)
	require.Empty(t, show.Error)
	var shown struct {
		Complete bool `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(show.Result, &shown))
	assert.True(t, shown.Complete)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "accepted")
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	show = runPassVM(
		t,
		f,
		19802,
		101,
		"Покажи мою прошлую регистрацию",
		`await tools.passes.registration.read({event:"archive",view:"home"}); return tools.passes.registration.show({event:"archive",view:"home"});`,
	)
	require.Empty(t, show.Error)
	handle(t, f.b, passMenuClick(t, f, 101, 19803, "Оплата"))
	assert.Contains(t, passMenuCard(t, f, 101).Text, "подтверждена")
	var mutable int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_buttons WHERE owner='alice' AND (action ? 'command' OR action ? 'profile' OR action ? 'assignment')`).
			Scan(&mutable),
	)
	assert.Zero(t, mutable)
	_, err = (passbooking.Service{DB: f.db}).Payment(t.Context(), "bob", "archive", "alice")
	require.Error(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.legacy_pass_payment_metadata WHERE event_id='archive'; DELETE FROM core.pass_bookings WHERE event_id='archive' AND owner='alice'`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, ""))
	assert.NotContains(t, passMenuCard(t, f, 101).Text, "подтверждена")
}

func TestScriptPassArchivedDiscoveryAndMutationBoundary(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	own := runPassVM(
		t,
		f,
		19810,
		101,
		"Which passes have I had?",
		`return tools.passes.events({});`,
	)
	require.Empty(t, own.Error)
	assert.Contains(t, string(own.Result), `"archive"`)
	foreign := runPassVM(
		t,
		f,
		19811,
		202,
		"Which passes have I had?",
		`return tools.passes.events({});`,
	)
	require.Empty(t, foreign.Error)
	assert.NotContains(t, string(foreign.Result), `"archive"`)
	read := runPassVM(
		t,
		f,
		19812,
		202,
		"Read archive payment",
		`return tools.passes.registration.read({event:"archive",view:"payment"});`,
	)
	assert.NotContains(t, string(read.Result), `"proof_reference"`)
	assert.Contains(t, string(read.Result), "unknown_event")
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='alice'`)
	require.NoError(t, err)
	blocked := runPassVM(
		t,
		f,
		19813,
		101,
		"Change archive payment contact",
		`await tools.passes.registration.read({event:"archive",view:"home"}); return tools.passes.registration.payment_admin({event:"archive",payment_admin:"bob"});`,
	)
	assert.NotEmpty(t, blocked.Error)
	var version int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT version FROM core.pass_bookings WHERE event_id='archive' AND owner='alice'`).
			Scan(&version),
	)
	assert.EqualValues(t, 1, version)
}
