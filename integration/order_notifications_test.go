package integration_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func pendingOrderNotice(t *testing.T) (paymentFixture, db.ProfileScope, db.OrderNotification) {
	t.Helper()
	f, owner, order := orderEvidenceFixture(t)
	f.H.stop(t)
	args := orderEvidenceArgs(t, f, order, 99700, "notice-proof")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), owner, uuid.New(), args)
	require.NoError(t, err)
	var id uuid.UUID
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT id FROM shuttle_order_evidence_notices WHERE evidence_id=$1 AND recipient_id=$2", evidence.ID, owner.UserID).
		Scan(&id))
	scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	n, err := f.H.store.OrderNotification(t.Context(), scope, id)
	require.NoError(t, err)
	return f, owner, n
}

func TestOrderNoticeStorePersistsAttemptAndNeverAutomaticallyRetriesUnknown(t *testing.T) {
	t.Parallel()
	f, owner, notice := pendingOrderNotice(t)
	scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	actor := paymentScope(t, f.H, owner.UserID, false)
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	saved, err := reopened.OrderNotification(t.Context(), scope, notice.ID)
	require.NoError(t, err)
	require.Equal(t, notice, saved)
	jobs, err := reopened.ReadyOrderNotifications(t.Context(), scope, uuid.Nil)
	require.NoError(t, err)
	require.Contains(t, jobs, db.NotificationJob{ID: notice.ID, Version: 1})
	payload := json.RawMessage(`{"chat_id":11002,"text":"Synthetic notice"}`)
	first, err := reopened.ReserveOrderNotification(t.Context(), actor, notice.ID, 1, payload)
	require.NoError(t, err)
	require.Equal(t, "send_inflight", first.State)
	require.NotEqual(t, uuid.Nil, first.AttemptID)
	_, err = reopened.ReserveOrderNotification(t.Context(), actor, notice.ID, 1, payload)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, reopened.FinishOrderNotification(t.Context(), first, "retryable", nil, 0))
	retry, err := reopened.OrderNotification(t.Context(), scope, notice.ID)
	require.NoError(t, err)
	second, err := reopened.ReserveOrderNotification(t.Context(), actor, notice.ID, retry.Version, payload)
	require.NoError(t, err)
	require.NotEqual(t, first.AttemptID, second.AttemptID)
	require.ErrorIs(t, reopened.FinishOrderNotification(t.Context(), first, "delivered", nil, 0), db.ErrConflict)
	require.NoError(t, reopened.RecoverOrderNotifications(t.Context(), scope))
	unknown, err := reopened.OrderNotification(t.Context(), scope, notice.ID)
	require.NoError(t, err)
	require.Equal(t, "delivery_unknown", unknown.State)
	require.Equal(t, second.AttemptID, unknown.AttemptID)
	require.Equal(t, notice.Evidence, unknown.Evidence)
	require.Equal(t, notice.Order, unknown.Order)
	require.NoError(t, reopened.RecoverOrderNotifications(t.Context(), scope))
	again, err := reopened.OrderNotification(t.Context(), scope, notice.ID)
	require.NoError(t, err)
	require.Equal(t, unknown, again)
	jobs, err = reopened.ReadyOrderNotifications(t.Context(), scope, uuid.Nil)
	require.NoError(t, err)
	for _, job := range jobs {
		require.NotEqual(t, notice.ID, job.ID)
	}
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
	raw, err := migrations.Files.ReadFile("00044_order_notice_delivery.sql")
	require.NoError(t, err)
	_, down, found := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, found)
	_, err = reopened.Pool().Exec(t.Context(), down)
	require.ErrorContains(t, err, "Export order notice delivery history before rollback")
}

func TestOrderNoticeStoreRechecksCurrentAccessBeforeReservation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"authority", "module", "owner_revoked", "recipient_revoked", "pass_unassigned", "subject", "deployment"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, owner, notice := pendingOrderNotice(t)
			scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
			id := notice.ID
			actor := paymentScope(t, f.H, owner.UserID, false)
			if scenario == "authority" || scenario == "recipient_revoked" {
				require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
					"SELECT id FROM shuttle_order_evidence_notices WHERE evidence_id=$1 AND recipient_id=$2", notice.EvidenceID, f.AmbassadorID).
					Scan(&id))
				actor = paymentScope(t, f.H, f.AmbassadorID, false)
			}
			// The worker's initial lookup succeeds before the later access change.
			_, _, err := f.H.store.OrderNotificationRecipient(t.Context(), scope, id)
			require.NoError(t, err)
			switch scenario {
			case "authority":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", notice.Evidence.EventID, actor.UserID)
			case "module":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'orders') WHERE id=$1", notice.Evidence.EventID)
			case "owner_revoked", "recipient_revoked":
				err = f.H.store.RevokeUser(t.Context(), actor.UserID, true)
			case "pass_unassigned":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AND user_id=$2", notice.Evidence.EventID, actor.UserID)
			case "subject":
				actor.Subject = "another-subject"
			case "deployment":
				actor.Organization = "another-organization"
			}
			require.NoError(t, err)
			reserved, err := f.H.store.ReserveOrderNotification(t.Context(), actor, id, 1, json.RawMessage(`{}`))
			if scenario == "pass_unassigned" {
				require.NoError(t, err)
				require.Equal(t, "send_inflight", reserved.State)
			} else {
				require.Error(t, err)
				unchanged, readErr := f.H.store.OrderNotification(t.Context(), scope, id)
				require.NoError(t, readErr)
				require.Equal(t, "pending", unchanged.State)
				require.Equal(t, int64(1), unchanged.Version)
				require.Equal(t, uuid.Nil, unchanged.AttemptID)
			}
		})
	}
}

func TestOrderNoticeStoreKeepsSoldTermsAndSuppressesSupersededAdmission(t *testing.T) {
	t.Parallel()
	f, owner, notice := pendingOrderNotice(t)
	scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET name='Changed later',timezone='Asia/Tokyo' WHERE id=$1", notice.Evidence.EventID)
	require.NoError(t, err)
	_, _, decision := reviewOrderEvidence(t, f, notice.Evidence, db.OrderEvidenceConfirmed)
	_, _, err = f.H.store.OrderNotificationRecipient(t.Context(), scope, notice.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// Only the admission notice becomes blocked; the business decision survives.
	require.NoError(t, f.H.store.BlockOrderNotification(t.Context(), scope, notice.ID, notice.Version))
	blocked, err := f.H.store.OrderNotification(t.Context(), scope, notice.ID)
	require.NoError(t, err)
	require.Equal(t, "delivery_blocked", blocked.State)
	require.Equal(t, db.OrderEvidencePending, blocked.Evidence.State)
	var id uuid.UUID
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT id FROM shuttle_order_evidence_notices WHERE evidence_id=$1 AND recipient_id=$2 AND kind='confirmed'", notice.EvidenceID, owner.UserID).
		Scan(&id))
	confirmed, err := f.H.store.OrderNotification(t.Context(), scope, id)
	require.NoError(t, err)
	require.Equal(t, decision, confirmed.Evidence)
	require.Equal(t, notice.Order, confirmed.Order)
	require.NotEqual(t, "Changed later", confirmed.Order.Snapshot.EventName)
	require.NotEqual(t, "Asia/Tokyo", confirmed.Order.Snapshot.Quote.Timezone)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
	foreign := scope
	foreign.Organization = "foreign-organization"
	_, err = f.H.store.OrderNotification(t.Context(), foreign, id)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, f.H.store.RecoverOrderNotifications(t.Context(), foreign))
	stillPending, err := f.H.store.OrderNotification(t.Context(), scope, id)
	require.NoError(t, err)
	require.Equal(t, "pending", stillPending.State)
}
