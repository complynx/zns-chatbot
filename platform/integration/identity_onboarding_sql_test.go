package integration_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestOnboardingSQLFailureKeepsInboxAndRetriesReservedIdentity(t *testing.T) {
	t.Parallel()
	f, provider := onboardingInboxFixture(t, "en")
	client, delivered := provisioningTelegram(t, 95108, 202)
	f.b.TG = client
	input := message(9200, 95108, "/orders")
	input.Message.From.FirstName = "Synthetic"
	input.Message.From.LanguageCode = "en"
	_, err := f.db.Exec(t.Context(), `UPDATE bot.telegram_inbox SET payload=$1 WHERE update_id=9200`, input)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `CREATE FUNCTION core.fail_onboarding_test() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'synthetic transaction failure' USING ERRCODE='40001'; END $$;
 CREATE TRIGGER fail_onboarding_test BEFORE INSERT ON core.users FOR EACH ROW EXECUTE FUNCTION core.fail_onboarding_test()`)
	require.NoError(t, err)
	var failureStatus atomic.Int64
	f.b.Onboarding = func(ctx context.Context, user telegram.User) error {
		requestErr := f.b.API.ProvisionTelegram(ctx, 77, user)
		if problem, ok := errors.AsType[*core.ProblemError](requestErr); ok {
			failureStatus.Store(int64(problem.Status))
		}
		return requestErr
	}
	runInboxUntil(t, f, func() bool { return failureStatus.Load() != 0 })
	require.Equal(t, int64(http.StatusServiceUnavailable), failureStatus.Load())
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Equal(t, 2, pending)
	var reserved identityprovision.Binding
	var ready bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT owner,subject,ready FROM core.identity_provisioning WHERE telegram_id=95108`).
			Scan(&reserved.Owner, &reserved.Subject, &ready),
	)
	require.False(t, ready)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER fail_onboarding_test ON core.users`)
	require.NoError(t, err)
	completeInbox(t, f, 9202)
	actual, err := provider.service.EnsureTelegram(t.Context(), identityprovision.Telegram{ID: 95108})
	require.NoError(t, err)
	require.Equal(t, reserved, actual)
	require.Positive(t, delivered.Load())
	remote, ok := provider.service.Provider.(*provisioningProvider)
	require.True(t, ok)
	require.Equal(t, 1, remote.creates)
}
