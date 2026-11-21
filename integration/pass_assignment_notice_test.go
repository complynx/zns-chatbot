package integration_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPassAssignmentNoticeRechecksRegistrationModule(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	tiers := []registrations.Tier{{ID: "promo", Amount: 2, PriceMinor: 7000, Promo: true, StartsAt: start}}
	f := newPassQAFixture(t, tiers, false)
	active := f.event.Document
	active.Deposit = &registrations.DepositPolicy{MinimumMinor: 2000}
	f.saveEvent(t, active)

	enabled := f.namedUser(t, 29601)
	after := f.solo(t, enabled, f.click(t, enabled, f.menu(t, enabled)))
	visible := f.pass(t, enabled, "promo", 7000)
	f.text(t, enabled, after, "Your pass costs 70.00 BYN. The minimum deposit is 20.00 BYN")
	var state string
	require.Eventually(t, func() bool {
		return f.h.store.Pool().QueryRow(t.Context(),
			"SELECT state FROM shuttle_pass_assignment_notices WHERE pass_id=$1", visible.Pass.ID).Scan(&state) == nil &&
			state == "delivered"
	}, 8*time.Second, 25*time.Millisecond)

	holdAssignmentNoticeJobs(t, f)
	delayed := f.namedUser(t, 29602)
	f.solo(t, delayed, f.click(t, delayed, f.menu(t, delayed)))
	assigned := f.pass(t, delayed, "promo", 7000)
	require.NotNil(t, assigned.Pass.Deposit)
	require.EqualValues(t, 2000, assigned.Pass.Deposit.RequiredMinor)
	jobID := scheduledAssignmentNotice(t, f, assigned.Pass.ID)

	disabled := f.event.Document
	disabled.Modules = nil
	f.saveEvent(t, disabled)
	require.Empty(t, f.event.Document.Modules)
	releaseAssignmentNotice(t, f, jobID)
	requireAssignmentNoticeBlocked(t, f, delayed, assigned.Pass.ID)

	require.Equal(t, assigned, f.registration(t, delayed))
	require.Equal(t, visible, f.registration(t, enabled))
	status, raw, _ := appCall(
		t,
		f.h,
		http.MethodGet,
		nativePassPaymentHTTPPath+"targets",
		"",
		delayed.app.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.NotContains(t, string(raw), assigned.Pass.ID.String())
	f.noLoom(t)
}

func holdAssignmentNoticeJobs(t *testing.T, f *passQAFixture) {
	t.Helper()
	_, err := f.h.store.Pool().Exec(t.Context(), `
CREATE FUNCTION delay_pass_assignment_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.kind = 'zinusya_pass_assignment_notice' THEN
    NEW.state := 'scheduled';
    NEW.scheduled_at := clock_timestamp() + interval '10 minutes';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER delay_pass_assignment_notice BEFORE INSERT ON river_job
FOR EACH ROW EXECUTE FUNCTION delay_pass_assignment_notice();
`)
	require.NoError(t, err)
}

func scheduledAssignmentNotice(t *testing.T, f *passQAFixture, passID uuid.UUID) int64 {
	t.Helper()
	var jobID int64
	require.Eventually(t, func() bool {
		return f.h.store.Pool().QueryRow(t.Context(), `SELECT id FROM river_job
WHERE kind='zinusya_pass_assignment_notice' AND args->>'pass_id'=$1 AND state='scheduled'`,
			passID.String()).Scan(&jobID) == nil
	}, 8*time.Second, 25*time.Millisecond)
	return jobID
}

func releaseAssignmentNotice(t *testing.T, f *passQAFixture, jobID int64) {
	t.Helper()
	tag, err := f.h.store.Pool().Exec(t.Context(),
		"UPDATE river_job SET state='available',scheduled_at=clock_timestamp() WHERE id=$1", jobID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

func requireAssignmentNoticeBlocked(t *testing.T, f *passQAFixture, owner passQAUser, passID uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		var state string
		var hasReceipt bool
		err := f.h.store.Pool().QueryRow(t.Context(),
			"SELECT state,receipt IS NOT NULL FROM shuttle_pass_assignment_notices WHERE pass_id=$1",
			passID).Scan(&state, &hasReceipt)
		return err == nil && state == "delivery_blocked" && !hasReceipt
	}, 8*time.Second, 25*time.Millisecond)
	require.NotContains(t, strings.Join(sentNotifications(f.p, owner.telegramID), "\n"), "Your pass costs 70.00 BYN.")
}
