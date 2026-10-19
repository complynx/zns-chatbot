package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPairNotificationIntentsSurviveRestartWithoutDuplicatingDecisions(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{"accept", "decline", "revoke", "expired"} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := setupPair(t)
			f.H.stop(t)
			invitation := invitePair(t, f)
			notices, err := f.H.store.ReadyPairInvitationNotifications(ctx, f.A.EventScope, uuid.Nil)
			require.NoError(t, err)
			require.Len(t, notices, 1)
			require.Equal(t, f.B.UserID, notices[0].RecipientID)
			require.Equal(t, "pending", notices[0].Kind)
			require.Equal(t, invitation.Invitation.ID, notices[0].InvitationID)
			first := notices[0]
			actor := f.B
			args := db.PairInvitationDecision{
				EventID:         f.EventID,
				InvitationID:    invitation.Invitation.ID,
				ExpectedVersion: 1,
			}
			if decision == "accept" {
				args.Role = registrations.Follower
			}
			if decision == "revoke" {
				actor = f.A
			}
			if decision == "expired" {
				_, err = f.H.store.Pool().Exec(ctx,
					`UPDATE shuttle_pair_invitations SET created_at=clock_timestamp()-interval '2 seconds',
					expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, args.InvitationID)
				require.NoError(t, err)
			}
			applyPairNoticeDecision(t, f, actor, decision, args)
			f.H.stop(t)
			f.H.start(t)
			applyPairNoticeDecision(t, f, actor, decision, args)
			notices, err = f.H.store.ReadyPairInvitationNotifications(ctx, f.A.EventScope, uuid.Nil)
			require.NoError(t, err)
			require.Len(t, notices, 2)
			require.Contains(t, notices, first)
			var changed db.PairInvitationNotification
			for _, notice := range notices {
				if notice.ID != first.ID {
					changed = notice
				}
			}
			require.Equal(t, int64(2), changed.InvitationVersion)
			require.Equal(t, invitation.Invitation.ID, changed.InvitationID)
			expectedKind := map[string]string{"accept": "accepted", "decline": "declined", "revoke": "revoked", "expired": "expired"}[decision]
			require.Equal(t, expectedKind, changed.Kind)
			expectedRecipient := f.A.UserID
			if decision == "revoke" {
				expectedRecipient = f.B.UserID
			}
			require.Equal(t, expectedRecipient, changed.RecipientID)
			wrongScope := f.A.EventScope
			wrongScope.Organization = "another-organization"
			notices, err = f.H.store.ReadyPairInvitationNotifications(ctx, wrongScope, uuid.Nil)
			require.NoError(t, err)
			require.Empty(t, notices)
		})
	}
}

func awaitPairNotice(t *testing.T, f pairFixture, id uuid.UUID, state string) {
	t.Helper()
	var result db.PairInvitationDelivery
	require.Eventually(t, func() bool {
		var err error
		result, err = f.H.store.PairInvitationNotification(context.Background(), f.A.EventScope, id)
		return err == nil && result.State == state
	}, 8*time.Second, 25*time.Millisecond)
}

func pairPendingNotice(t *testing.T, f pairFixture) uuid.UUID {
	t.Helper()
	invitePair(t, f)
	rows, err := f.H.store.ReadyPairInvitationNotifications(context.Background(), f.A.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0].ID
}

func TestPairNotificationWaitsForIAMAndRetriesKnownRejection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	f.H.stop(t)
	_, err := f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_users SET profile='{"language_code":"uk"}' WHERE id=$1`, f.B.UserID)
	require.NoError(t, err)
	id := pairPendingNotice(t, f)
	p := f.H.peers
	p.mu.Lock()
	p.iamDown, p.loomDown, p.rejectSendOnce = true, true, true
	p.mu.Unlock()
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 12003)) > 0 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
	p.mu.Lock()
	p.iamDown = false
	p.mu.Unlock()
	awaitPairNotice(t, f, id, "retryable")
	awaitPairNotice(t, f, id, "delivered")
	messages := sentNotifications(p, 12003)
	require.Len(t, messages, 2)
	require.Equal(t, messages[0], messages[1])
	require.Contains(t, messages[1], "приглашает вас в пару")
	require.Contains(t, messages[1], "Europe/Minsk")
	require.Contains(t, messages[1], "+03:00")
	require.Contains(t, messages[1], f.EventID.String())
	f.H.stop(t)
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 12003)) > 2 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
}

func TestPairNotificationSuppressesStaleInvitationAndDeliversDecision(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	f.H.stop(t)
	invitation := invitePair(t, f)
	rows, err := f.H.store.ReadyPairInvitationNotifications(context.Background(), f.A.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	oldID := rows[0].ID
	_, err = f.H.store.DecidePairInvitation(
		context.Background(),
		f.B,
		"accept-before-notice",
		"accept",
		db.PairInvitationDecision{
			EventID:         f.EventID,
			InvitationID:    invitation.Invitation.ID,
			ExpectedVersion: 1,
			Role:            registrations.Follower,
		},
	)
	require.NoError(t, err)
	rows, err = f.H.store.ReadyPairInvitationNotifications(context.Background(), f.A.EventScope, uuid.Nil)
	require.NoError(t, err)
	var decisionID uuid.UUID
	for _, row := range rows {
		if row.Kind == "accepted" {
			decisionID = row.ID
		}
	}
	require.NotEqual(t, uuid.Nil, decisionID)
	f.H.start(t)
	awaitPairNotice(t, f, oldID, "delivery_blocked")
	awaitPairNotice(t, f, decisionID, "delivered")
	require.Empty(t, sentNotifications(f.H.peers, 12003))
	messages := sentNotifications(f.H.peers, 12002)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0], "invitation was accepted")
}

func TestPairNotificationDoesNotResendAfterLostTelegramResponse(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	f.H.stop(t)
	id := pairPendingNotice(t, f)
	f.H.peers.mu.Lock()
	f.H.peers.loseSend = true
	f.H.peers.mu.Unlock()
	f.H.start(t)
	awaitPairNotice(t, f, id, "delivery_unknown")
	require.Len(t, sentNotifications(f.H.peers, 12003), 1)
	f.H.stop(t)
	f.H.start(t)
	awaitPairNotice(t, f, id, "delivery_unknown")
	require.Never(
		t,
		func() bool { return len(sentNotifications(f.H.peers, 12003)) > 1 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
}

func TestPairNotificationBlocksRevokedRecipient(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	f.H.stop(t)
	id := pairPendingNotice(t, f)
	require.NoError(t, f.H.store.RevokeUser(context.Background(), f.B.UserID, true))
	f.H.start(t)
	awaitPairNotice(t, f, id, "delivery_blocked")
	require.Empty(t, sentNotifications(f.H.peers, 12003))
}

func TestPairNotificationReservationRechecksInvitationAfterDecision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	f.H.stop(t)
	id := pairPendingNotice(t, f)
	n, err := f.H.store.PairInvitationNotification(ctx, f.A.EventScope, id)
	require.NoError(t, err)
	_, err = f.H.store.PairNotificationRecipient(ctx, f.A.EventScope, n.PairInvitationNotification)
	require.NoError(t, err)
	_, err = f.H.store.DecidePairInvitation(ctx, f.B, "decline-before-reserve", "decline", db.PairInvitationDecision{
		EventID: f.EventID, InvitationID: n.InvitationID, ExpectedVersion: 1,
	})
	require.NoError(t, err)
	_, err = f.H.store.ReservePairNotification(
		ctx,
		f.B,
		n,
		json.RawMessage(`{"chat_id":12003,"text":"stale invitation"}`),
	)
	require.ErrorIs(t, err, db.ErrPairDenied)
	require.Empty(t, sentNotifications(f.H.peers, 12003))
}

func TestPairNotificationRestartAfterReservationDoesNotAssumeSendFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	f.H.stop(t)
	id := pairPendingNotice(t, f)
	n, err := f.H.store.PairInvitationNotification(ctx, f.A.EventScope, id)
	require.NoError(t, err)
	reserved, err := f.H.store.ReservePairNotification(
		ctx,
		f.B,
		n,
		json.RawMessage(`{"chat_id":12003,"text":"reserved before restart"}`),
	)
	require.NoError(t, err)
	require.Equal(t, "send_inflight", reserved.State)
	require.NotEqual(t, uuid.Nil, reserved.AttemptID)
	f.H.start(t)
	awaitPairNotice(t, f, id, "delivery_unknown")
	require.Never(
		t,
		func() bool { return len(sentNotifications(f.H.peers, 12003)) > 0 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
}

func applyPairNoticeDecision(
	t *testing.T,
	f pairFixture,
	actor db.PaymentScope,
	decision string,
	args db.PairInvitationDecision,
) {
	t.Helper()
	ctx := context.Background()
	if decision == "expired" {
		_, err := f.H.store.ExpirePairInvitations(ctx, f.A.EventScope, uuid.Nil)
		require.NoError(t, err)
		return
	}
	_, err := f.H.store.DecidePairInvitation(ctx, actor, "notice-decision", decision, args)
	require.NoError(t, err)
}

func TestPairInvitationRollsBackWhenItsDeliveryIntentCannotBeSaved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	f.H.stop(t)
	_, err := f.H.store.Pool().Exec(ctx, `CREATE FUNCTION reject_pair_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'synthetic notice storage failure'; END; $$;
CREATE TRIGGER reject_pair_notice BEFORE INSERT ON shuttle_pair_notifications
FOR EACH ROW EXECUTE FUNCTION reject_pair_notice();`)
	require.NoError(t, err)
	args := db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version}
	_, err = f.H.store.CreatePairInvitation(ctx, f.A, "atomic-notice", args)
	require.Error(t, err)
	entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	require.Equal(t, f.Entry.Version, entry.Version)
	invitations, err := f.H.store.PairInvitations(ctx, f.B, db.PairInvitationList{Direction: "incoming", Limit: 20})
	require.NoError(t, err)
	require.Empty(t, invitations)
	_, err = f.H.store.Pool().
		Exec(ctx, "DROP TRIGGER reject_pair_notice ON shuttle_pair_notifications; DROP FUNCTION reject_pair_notice()")
	require.NoError(t, err)
	result, err := f.H.store.CreatePairInvitation(ctx, f.A, "atomic-notice", args)
	require.NoError(t, err)
	require.Equal(t, "pending", result.Invitation.State)
	notices, err := f.H.store.ReadyPairInvitationNotifications(ctx, f.A.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, notices, 1)
}
