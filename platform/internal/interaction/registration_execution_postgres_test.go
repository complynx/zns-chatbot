package interaction_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type registrationDomainPorts struct {
	interaction.DerivedRegistrationClient
	interaction.RegistrationReceiptClient

	service derivedmutation.Service
	calls   int
}

func (p *registrationDomainPorts) ExecuteDerivedPassBooking(ctx context.Context, owner string,
	command passbooking.Command, source readsource.Derivation,
) (passbooking.Booking, error) {
	p.calls++
	return p.service.ExecutePassBooking(ctx, owner, command, source)
}

func (p *registrationDomainPorts) PassBookingReceipt(ctx context.Context, owner string,
	command passbooking.Command, _ readsource.Derivation,
) (derivedmutation.Receipt[passbooking.Booking], error) {
	return p.service.PassBookingReceipt(ctx, owner, command)
}

func registrationRecordWriter(db *pgxpool.Pool, id int64) interaction.RegistrationExecutionWriter {
	return func(ctx context.Context, owner string, record interaction.RegistrationExecutionRecord) error {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		_, err = db.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES($1,$2,'registration_action',$3) ON CONFLICT DO NOTHING`, owner, id, raw)
		return err
	}
}

func TestRegistrationExecutionPostgresReceiptBeforeMetadataFailure(t *testing.T) {
	t.Parallel()
	db := turnDatabase(t)
	_, err := db.Exec(t.Context(), `
 INSERT INTO core.pass_events(id,finishes_at) VALUES('execution',now()+interval '30 days');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('execution',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('execution','bob');
 INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader');
 CREATE FUNCTION bot.reject_registration_metadata() RETURNS trigger LANGUAGE plpgsql AS
 $$BEGIN RAISE EXCEPTION 'synthetic registration metadata failure'; END$$;
 CREATE TRIGGER reject_registration_metadata BEFORE INSERT ON bot.interactions
 FOR EACH ROW WHEN (NEW.kind='registration_action') EXECUTE FUNCTION bot.reject_registration_metadata();`)
	require.NoError(t, err)
	command := passbooking.Command{Key: "saved-independent-key", Name: "solo", Event: "execution"}
	plan := interaction.SavedPlan{
		RegistrationCommand: &command,
		PassAuthority: &interaction.PlanAuthority{
			ReadAuthorities: []readsource.Authority{}, Reads: []interaction.PassContextDependency{},
		},
	}
	plan.BindKind()
	store := interaction.Store{DB: db}
	saved, err := store.SaveWinner(t.Context(), "alice", 71, plan)
	require.NoError(t, err)
	source := readsource.Derivation{
		Generation:  &saved.HistoryGeneration,
		Authorities: saved.PassAuthority.ReadAuthorities,
	}
	ports := &registrationDomainPorts{
		service: derivedmutation.Service{DB: db, Registration: passbooking.Service{DB: db}},
	}
	executor := interaction.RegistrationExecutor{Derived: ports, Writer: registrationRecordWriter(db, 71)}
	committed, err := executor.Command(t.Context(), "alice", *saved.RegistrationCommand, &source)
	require.ErrorIs(t, err, interaction.ErrRegistrationExecutionRecord)
	require.Positive(t, committed.Version)
	require.Equal(t, 1, ports.calls)
	assertRegistrationExecutionCounts(t, db, 1, 0)
	_, err = db.Exec(t.Context(), `DROP TRIGGER reject_registration_metadata ON bot.interactions`)
	require.NoError(t, err)

	// Reconstruct from the persisted plan. This executor has no command port.
	restored, err := (interaction.Store{DB: db}).Load(t.Context(), "alice", 71)
	require.NoError(t, err)
	require.Equal(t, command, *restored.RegistrationCommand)
	restarted := interaction.RegistrationExecutor{Receipts: ports, Writer: registrationRecordWriter(db, 71)}
	for range 2 {
		receipt, recoverErr := restarted.RecoverCommand(t.Context(), "alice", *restored.RegistrationCommand, source)
		require.NoError(t, recoverErr)
		require.True(t, receipt.Found)
		require.Equal(t, committed, receipt.Result)
	}
	require.Equal(t, 1, ports.calls)
	assertRegistrationExecutionCounts(t, db, 1, 1)
	current, err := (passbooking.Service{DB: db}).Get(t.Context(), "alice", "execution")
	require.NoError(t, err)
	require.Equal(t, committed.Version, current.Version)
}

func assertRegistrationExecutionCounts(t *testing.T, db *pgxpool.Pool, receipts, records int) {
	t.Helper()
	var actualReceipts, actualRecords int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations
 WHERE event_id='execution' AND actor='alice'`).Scan(&actualReceipts))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions
 WHERE owner='alice' AND update_id=71 AND kind='registration_action' AND content->>'committed'='true'`).
		Scan(&actualRecords))
	require.Equal(t, receipts, actualReceipts)
	require.Equal(t, records, actualRecords)
}
