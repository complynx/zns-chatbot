package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsExplicitCutoverDoesNotEnforceLegacyBeforeEpoch(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.CreditsEnforce = true
	s := credits.Service{DB: f.db, Enforce: true}
	installCreditPrice(t, s)
	f.b.Model = meteredRemoteFixture{s}
	handle(t, f.b, message(98501, 101, "Before explicit cutover"))
	var mode string
	var enforced bool
	var questions int
	require.NoError(t,
		f.db.QueryRow(t.Context(), `SELECT mode FROM bot.budget_operations WHERE owner='alice' AND update_id=98501`).
			Scan(&mode))
	require.NoError(t,
		f.db.QueryRow(t.Context(), `SELECT enforced FROM credits.attempts WHERE operation_key='telegram:98501'`).
			Scan(&enforced))
	require.NoError(t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.agent_quota WHERE owner='alice' AND update_id=98501`).
			Scan(&questions))
	require.Equal(t, "legacy", mode)
	require.Equal(t, 1, questions)
	require.False(t, enforced, "explicit cutover has not happened and durable update mode is legacy")
}
func TestCreditsUnlimitedPolicySourceIsAccount(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	s := credits.Service{DB: f.db}
	_, err = s.SetPolicy(
		t.Context(),
		"bob",
		"alice",
		credits.PolicyChange{Unlimited: true, Version: 1, OperationKey: "qa:unlimited"},
	)
	require.NoError(t, err)
	for index, language := range []string{"en", "ru"} {
		_, err = f.b.API.SetLanguage(t.Context(), "alice", language, false)
		require.NoError(t, err)
		handle(t, f.b, message(98601+int64(index), 101, "/usage"))
		messages := chatMessages(t, f, 101)
		got := messages[len(messages)-1].Text
		want := "account policy"
		if language == "ru" {
			want = "политика аккаунта"
		}
		require.Contains(t, got, want)
	}
}
