package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsManualLocalesAndCurrentAdmin(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", language, false)
			require.NoError(t, err)
			handle(t, f.b, message(8401, 101, "/usage"))
			messages := chatMessages(t, f, 101)
			require.Len(t, messages, 1)
			require.Contains(t, messages[0].Text, "1")
			if language == "en" {
				require.Contains(t, messages[0].Text, "UTC month")
				require.Contains(t, messages[0].Text, "Monthly allowance: 1 (default policy)")
			} else {
				require.Contains(t, messages[0].Text, "UTC")
				require.Contains(t, messages[0].Text, "Месячный лимит: 1 (политика по умолчанию)")
			}
			handle(t, f.b, message(8402, 101, "/credits_policy"))
			messages = chatMessages(t, f, 101)
			require.NotContains(t, messages[len(messages)-1].Text, "/credits_user")
			_, err = f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
			require.NoError(t, err)
			handle(t, f.b, message(8403, 101, "/credits_policy bob 0.123456789"))
			report, err := (credits.Service{DB: f.db}).Usage(t.Context(), "bob", "bob")
			require.NoError(t, err)
			require.Equal(t, int64(123456789), report.Policy.MonthlyNanoUSD)
			handle(t, f.b, message(8405, 101, "/credits_user bob"))
			messages = chatMessages(t, f, 101)
			require.Contains(t, messages[len(messages)-1].Text, "0.123456789")
			if language == "en" {
				require.Contains(t, messages[len(messages)-1].Text, "account policy")
			} else {
				require.Contains(t, messages[len(messages)-1].Text, "политика аккаунта")
			}
			_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
			require.NoError(t, err)
			handle(t, f.b, message(8404, 101, "/credits_policy bob unlimited"))
			report, err = (credits.Service{DB: f.db}).Usage(t.Context(), "bob", "bob")
			require.NoError(t, err)
			require.False(t, report.Policy.Unlimited)
		})
	}
}
