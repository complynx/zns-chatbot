package integration_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRegistrationBotMetadataFailureRecoversWithoutModel(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"explicit-saved-key", ""} {
		t.Run("saved-key="+key, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			service := passbooking.Service{DB: f.db}
			current, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			command := bookingCommand("cancel", key, current)
			plan := interaction.SavedPlan{
				RegistrationCommand: &command,
				PassAuthority: &interaction.PlanAuthority{
					Reads: []interaction.PassContextDependency{}, ReadAuthorities: []readsource.Authority{},
				},
				Plan: agent.Plan{View: agent.RegistrationView},
			}
			plan.BindKind()
			_, err = (interaction.Store{DB: f.db}).SaveWinner(t.Context(), "alice", 99201, plan)
			require.NoError(t, err)
			rejectRegistrationMetadata(t, f)
			before := chatMessages(t, f, 101)
			update := message(99201, 101, "apply the persisted choice")
			err = f.b.Handle(t.Context(), update)
			require.ErrorIs(t, err, interaction.ErrRegistrationExecutionRecord)
			require.Zero(t, f.model.calls, "the saved command must not ask the model")
			require.Equal(t, before, chatMessages(t, f, 101), "failed metadata must not publish a success reply")
			committed, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			require.Equal(t, "cancelled", committed.State)
			require.Greater(t, committed.Version, current.Version)
			exactKey := key
			if exactKey == "" {
				exactKey = "tg-registration-99201"
			}
			assertRegistrationBotExecution(t, f, exactKey, 0)
			_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_registration_metadata ON bot.interactions`)
			require.NoError(t, err)

			// A new Bot value keeps the durable DB and real HTTP adapters. No new model plan is provided.
			restarted := *f.b
			f.b = &restarted
			require.NoError(t, restarted.Handle(t.Context(), update))
			drainPassNotices(t, f)
			require.Zero(t, f.model.calls)
			assertRegistrationBotExecution(t, f, exactKey, 1)
			visible := chatMessages(t, f, 101)
			require.NotEmpty(t, visible)
			require.NotEmpty(t, passMenuCard(t, f, 101).Text)
			require.NoError(t, restarted.Handle(t.Context(), update))
			drainPassNotices(t, f)
			require.Equal(t, visible, chatMessages(t, f, 101), "duplicate ingress must not add a second reply")
			require.Zero(t, f.model.calls)
			after, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			require.Equal(t, committed.Version, after.Version)
			assertRegistrationBotExecution(t, f, exactKey, 1)
		})
	}
}

func rejectRegistrationMetadata(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `
 CREATE FUNCTION bot.reject_registration_metadata() RETURNS trigger LANGUAGE plpgsql AS
 $$BEGIN RAISE EXCEPTION 'synthetic registration metadata failure'; END$$;
 CREATE TRIGGER reject_registration_metadata BEFORE INSERT ON bot.interactions
 FOR EACH ROW WHEN (NEW.kind='registration_action') EXECUTE FUNCTION bot.reject_registration_metadata();`)
	require.NoError(t, err)
}

func assertRegistrationBotExecution(t *testing.T, f *fixture, key string, completed int) {
	t.Helper()
	var receipts, metadata, replies, archives int
	keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations
 WHERE event_id='dance' AND actor='alice' AND key_hash=$1`, keyHash).Scan(&receipts))
	require.Equal(t, 1, receipts, "the exact original key has one canonical receipt")
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 count(*) FILTER (WHERE kind='registration_action' AND content->>'committed'='true'),
 count(*) FILTER (WHERE kind='registration_reply')
 FROM bot.interactions WHERE owner='alice' AND update_id=99201`).Scan(&metadata, &replies))
	require.Equal(t, completed, metadata)
	require.Equal(t, completed, replies)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events
 WHERE owner='alice' AND source_key='tg-assistant-99201'`).Scan(&archives))
	require.Equal(t, completed, archives)
}
