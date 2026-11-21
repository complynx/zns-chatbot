package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPassAssignmentNoticeRechecksCommittedReturn(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	tiers := []registrations.Tier{{ID: "promo", Amount: 1, PriceMinor: 7000, Promo: true, StartsAt: start}}
	f := newPassQAFixture(t, tiers, false)
	owner := f.namedUser(t, 29611)
	f.p.mu.Lock()
	f.p.adminRole = true
	f.p.mu.Unlock()
	admin := detailsAppSession(t, f.h, 29612, 211)
	holdAssignmentNoticeJobs(t, f)
	f.solo(t, owner, f.click(t, owner, f.menu(t, owner)))
	assigned := f.pass(t, owner, "promo", 7000)
	jobID := scheduledAssignmentNotice(t, f, assigned.Pass.ID)

	// Block the return's registration write after it has acquired the event lock.
	pool := f.h.store.Pool()
	gate, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = gate.Rollback(context.Background()) }()
	_, err = gate.Exec(t.Context(), "SELECT id FROM shuttle_registrations WHERE id=$1 FOR UPDATE", assigned.ID)
	require.NoError(t, err)
	change := db.PassAdminChange{OperationID: uuid.New(), RegistrationID: assigned.ID,
		ExpectedVersion: assigned.Version, Reason: "Synthetic return"}
	body := paymentAppJSON(t, struct {
		db.PassAdminChange

		Confirmed bool `json:"confirmed"`
	}{change, true})
	type response struct {
		status int
		err    error
	}
	done := make(chan response, 1)
	go func() {
		status, _, _, callErr := appHTTPCall(f.h, http.MethodPost, "/app/pass-admin/events/"+f.event.ID.String()+"/return",
			body, admin.Token, appTestOrigin)
		done <- response{status, callErr}
	}()

	blockedBackend := func(blocker uint32, queryPattern string) uint32 {
		t.Helper()
		var pid uint32
		require.Eventually(t, func() bool {
			return pool.QueryRow(t.Context(), `SELECT pid FROM pg_stat_activity
WHERE application_name=current_schema() AND $1::integer=ANY(pg_blocking_pids(pid)) AND query ~ $2`,
				blocker, queryPattern).Scan(&pid) == nil
		}, 8*time.Second, 25*time.Millisecond)
		return pid
	}
	returning := blockedBackend(gate.Conn().PgConn().PID(), "UPDATE shuttle_registrations")
	releaseAssignmentNotice(t, f, jobID)
	blockedBackend(returning, "shuttle_(pass_assignment_notices|events)")
	require.NoError(t, gate.Commit(t.Context()))
	result := <-done
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.status)
	require.Equal(t, registrations.Returned, f.registration(t, owner).State)
	requireAssignmentNoticeBlocked(t, f, owner, assigned.Pass.ID)
}
