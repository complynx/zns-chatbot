package integration_test

import (
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/account"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestBroadcastProfileExplicitWritesAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.admin_broadcast_profiles(owner,source_key,source_hash,fields)
 VALUES('alice','source','hash','{"username":null,"inner_name_de":"Quelle","language_code":null}')`)
	require.NoError(t, err)
	s := account.Service{DB: f.db}
	_, err = s.SetLanguage(t.Context(), "alice", "en", true)
	require.NoError(t, err)
	var overrides string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT overrides::text FROM core.admin_broadcast_profiles WHERE owner='alice'`).
			Scan(&overrides),
	)
	assert.JSONEq(t, `{}`, overrides, "initialization must not project an existing default column")
	_, err = s.SetLanguageWithOperation(t.Context(), "alice", "ru", false, "one")
	require.NoError(t, err)
	_, err = s.SetLanguageWithOperation(t.Context(), "alice", "en", false, "two")
	require.NoError(t, err)
	_, err = s.SetLanguageWithOperation(t.Context(), "alice", "ru", false, "one")
	require.NoError(t, err)
	require.NoError(
		t,
		s.RefreshTelegramMetadata(
			t.Context(),
			"alice",
			account.TelegramMetadataUpdate{UpdateID: 50, Sender: account.SenderMetadata{ID: 101, FirstName: "Current"}},
		),
	)
	require.NoError(
		t,
		s.RefreshTelegramMetadata(
			t.Context(),
			"alice",
			account.TelegramMetadataUpdate{UpdateID: 49, Sender: account.SenderMetadata{ID: 101, FirstName: "Stale"}},
		),
	)
	p := passes.Service{DB: f.db}
	_, err = p.Execute(
		t.Context(),
		"alice",
		passes.Command{Name: "set", Field: "legal_name", Value: "Legal", Key: "legal", Origin: "manual"},
	)
	require.NoError(t, err)
	var fields string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT fields::text,overrides::text FROM core.admin_broadcast_profiles WHERE owner='alice'`).
			Scan(&fields, &overrides),
	)
	assert.JSONEq(t, `{"username":null,"inner_name_de":"Quelle","language_code":null}`, fields)
	assert.JSONEq(
		t,
		`{"user_id":101,"username":"","first_name":"Current","last_name":"","print_name":"Current","language_code":"en","legal_name":"Legal"}`,
		overrides,
	)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language='' WHERE id='bob'`)
	require.NoError(t, err)
	_, err = s.SetLanguage(t.Context(), "bob", "", true)
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_broadcast_profiles WHERE owner='bob'`).Scan(&count),
	)
	assert.Zero(t, count, "an absent Telegram locale must not create a default projection value")
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language='' WHERE id='bob'`)
	require.NoError(t, err)
	_, err = s.SetLanguage(t.Context(), "bob", "de-DE", true)
	require.NoError(t, err)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT overrides::text FROM core.admin_broadcast_profiles WHERE owner='bob'`).
			Scan(&overrides),
	)
	assert.JSONEq(
		t,
		`{"language_code":"de-DE"}`,
		overrides,
		"retain the authoritative locale before presentation fallback",
	)
}

func TestBroadcastProfileMissingImportCannotBecomeNative(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_user_references(source_key,owner,source_record_sha256) VALUES($1,'alice',$2)`,
		strings.Repeat("a", 64),
		strings.Repeat("b", 64),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.admin_broadcast_profiles WHERE owner='alice'`)
	require.NoError(t, err)
	s := account.Service{DB: f.db}
	err = s.RefreshTelegramMetadata(
		t.Context(),
		"alice",
		account.TelegramMetadataUpdate{UpdateID: 80, Sender: account.SenderMetadata{ID: 101, FirstName: "Uncommitted"}},
	)
	require.EqualError(t, err, "broadcast_profile_missing")
	var update int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT telegram_metadata_update FROM core.users WHERE id='alice'`).Scan(&update),
	)
	assert.Less(t, update, int64(80))
	before, err := s.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	_, err = s.SetLanguageWithOperation(t.Context(), "alice", "ru", false, "missing-profile")
	require.EqualError(t, err, "broadcast_profile_missing")
	after, err := s.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, before, after, "projection failure must roll back the account write")
	var receipts int
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		`SELECT count(*) FROM core.language_operations WHERE owner='alice' AND operation_key='missing-profile'`,
	).Scan(&receipts))
	assert.Zero(t, receipts, "projection failure must not retain the operation identity")
}
