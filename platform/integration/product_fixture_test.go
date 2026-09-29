package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestProductFixturePreservesAcceptanceChanges(t *testing.T) {
	t.Parallel()
	db := database(t)
	require.NoError(t, sandbox.ApplyProductFixture(t.Context(), db))
	parties, err := (massage.Service{DB: db}).Parties(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, parties, 1)
	assert.False(t, parties[0].Open, "open-social parties are excluded from massage registration")
	_, err = db.Exec(t.Context(), `UPDATE core.knowledge_facts SET body='QA changed',version=2
 WHERE scope='sandbox-festival';
 DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review';
 UPDATE core.massage_specialists SET notify_next=false WHERE owner='bob'`)
	require.NoError(t, err)
	require.NoError(t, sandbox.ApplyProductFixture(t.Context(), db))
	var body string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT body FROM core.knowledge_facts WHERE scope='sandbox-festival'`).Scan(&body))
	assert.Equal(t, "QA changed", body)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_work`).Scan(&count))
	assert.Equal(t, 1, count)
	var notify bool
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT notify_next FROM core.massage_specialists WHERE owner='bob'`).Scan(&notify))
	assert.False(t, notify)
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT count(*) FROM core.knowledge_permissions WHERE permission='review'`).Scan(&count))
	assert.Zero(t, count)
}

func TestProductFixtureAddsPassportScenarioOnceToExistingRun(t *testing.T) {
	t.Parallel()
	db := database(t)
	_, err := db.Exec(
		t.Context(),
		`CREATE TABLE public.zns_sandbox_fixtures(name text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now());
 INSERT INTO public.zns_sandbox_fixtures(name) VALUES('product-v1');
 INSERT INTO core.pass_events(id,finishes_at,titles) VALUES('sandbox-festival',now()+interval '1 day','{"en":"QA title"}');
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
 VALUES('sandbox-festival','alice',7,'paid','leader','solo','bob',now(),now(),123);
 INSERT INTO core.pass_profiles(owner,legal_name,passport,frozen) VALUES('alice','Synthetic Existing','TEST ONLY',true)`,
	)
	require.NoError(t, err)
	require.NoError(t, sandbox.ApplyProductFixture(t.Context(), db))
	var required bool
	var english, russian string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT passport_required,titles->>'en',titles->>'ru'
 FROM core.pass_events WHERE id='sandbox-passport-pair'`).Scan(&required, &english, &russian))
	assert.True(t, required)
	assert.Equal(t, "Passport and partner practice", english)
	assert.Equal(t, "Паспорт и пара: учебное событие", russian)
	var tiers, places, bookings int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*),sum(amount) FROM core.pass_event_tiers
 WHERE event_id='sandbox-passport-pair' AND price>0 AND starts_at<now()`).Scan(&tiers, &places))
	assert.Equal(t, 2, tiers)
	assert.Equal(t, 200, places)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings WHERE event_id='sandbox-passport-pair'`).
			Scan(&bookings),
	)
	assert.Zero(t, bookings)
	var admin string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT owner FROM core.pass_payment_admins WHERE event_id='sandbox-passport-pair'`).
			Scan(&admin),
	)
	assert.Equal(t, "bob", admin)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=now()-interval '1 day',passport_required=false WHERE id='sandbox-passport-pair';
 DELETE FROM core.pass_event_tiers WHERE event_id='sandbox-passport-pair';
 DELETE FROM core.pass_payment_admins WHERE event_id='sandbox-passport-pair';
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('sandbox-passport-pair','bob',9,'waitlist','follower','solo','bob',now())`,
	)
	require.NoError(t, err)
	require.NoError(t, sandbox.ApplyProductFixture(t.Context(), db))
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_event_tiers WHERE event_id='sandbox-passport-pair'`).
			Scan(&tiers),
	)
	assert.Zero(t, tiers)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_admins WHERE event_id='sandbox-passport-pair'`).
			Scan(&bookings),
	)
	assert.Zero(t, bookings)
	var preserved bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT finishes_at<now() AND NOT passport_required FROM core.pass_events WHERE id='sandbox-passport-pair') AND
 (SELECT state='paid' AND price=123 AND version=7 FROM core.pass_bookings WHERE event_id='sandbox-festival' AND owner='alice') AND
 (SELECT version=9 FROM core.pass_bookings WHERE event_id='sandbox-passport-pair' AND owner='bob') AND
 (SELECT frozen AND passport='TEST ONLY' AND legal_name='Synthetic Existing' FROM core.pass_profiles WHERE owner='alice')`).Scan(&preserved))
	assert.True(t, preserved)
}

func TestProductFixtureDoesNotModifyExistingPassportEvent(t *testing.T) {
	t.Parallel()
	db := database(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at,titles,passport_required)
 VALUES('sandbox-passport-pair',now()-interval '1 day','{"en":"Existing event"}',false)`)
	require.NoError(t, err)
	require.NoError(t, sandbox.ApplyProductFixture(t.Context(), db))
	var preserved bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT finishes_at<now() AND NOT passport_required AND titles->>'en'='Existing event'
 AND NOT EXISTS(SELECT 1 FROM core.pass_event_tiers WHERE event_id='sandbox-passport-pair')
 AND NOT EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE event_id='sandbox-passport-pair')
 FROM core.pass_events WHERE id='sandbox-passport-pair'`).Scan(&preserved),
	)
	assert.True(t, preserved)
}
