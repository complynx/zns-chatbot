package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

type massageCrashRequest struct {
	DSN          string                          `json:"dsn"`
	BotID        int64                           `json:"bot_id"`
	Organization string                          `json:"organization"`
	Subject      string                          `json:"subject"`
	UserID       uuid.UUID                       `json:"user_id"`
	Operation    uuid.UUID                       `json:"operation_id"`
	Proposal     uuid.UUID                       `json:"proposal_id"`
	Kind         string                          `json:"kind"`
	Book         db.MassageBookingArguments      `json:"book"`
	Cancel       db.MassageCancellationArguments `json:"cancel"`
}

func (r massageCrashRequest) apply(t *testing.T, store *db.Store) db.MassageBooking {
	t.Helper()
	scope := db.ProfileScope{BotID: r.BotID, Organization: r.Organization, Subject: r.Subject, UserID: r.UserID}
	if r.Kind == "book" {
		result, err := store.BookOwnMassage(
			t.Context(),
			scope,
			r.Operation,
			r.Proposal,
			r.Book,
			db.MassageSourceAgent,
		)
		require.NoError(t, err)
		return result
	}
	result, err := store.CancelOwnMassage(t.Context(), scope, r.Operation, r.Proposal, r.Cancel)
	require.NoError(t, err)
	return result
}

// The parent kills this process at a real transaction boundary. PostgreSQL stays up.
func massageCrashChild(t *testing.T) {
	t.Helper()
	var request massageCrashRequest
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("ZINUSYA_MASSAGE_CRASH_REQUEST")), &request))
	store, err := db.Open(t.Context(), request.DSN)
	require.NoError(t, err)
	defer store.Close()
	request.apply(t, store)
	_, err = io.WriteString(os.Stdout, "massage committed\n")
	require.NoError(t, err)
	<-t.Context().Done()
}

func TestMassageProcessCrashRollsBackOrRecoversCommittedOperation(t *testing.T) {
	t.Parallel()
	if os.Getenv("ZINUSYA_MASSAGE_CRASH_CHILD") == "1" {
		massageCrashChild(t)
		return
	}
	for _, kind := range []string{"book", "cancel"} {
		for _, boundary := range []string{"before_commit", "after_commit"} {
			t.Run(kind+"/"+boundary, func(t *testing.T) {
				t.Parallel()
				checkMassageProcessCrash(t, kind, boundary)
			})
		}
	}
}

func checkMassageProcessCrash(t *testing.T, kind, boundary string) {
	t.Helper()
	f, owner, _, schedule := massageBookingFixture(t, 1)
	request := massageCrashRequest{
		BotID:        owner.BotID,
		Organization: owner.Organization,
		Subject:      owner.Subject,
		UserID:       owner.UserID,
		Operation:    uuid.New(),
		Kind:         kind,
		Book:         massageBookingArgs(f, schedule, 0),
	}
	var initial db.MassageBooking
	if kind == "book" {
		proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, request.Operation, request.Book)
		require.NoError(t, err)
		request.Proposal = proposal.ID
	} else {
		initial, _ = bookMassage(t, f, owner, request.Book)
		request.Cancel = db.MassageCancellationArguments{
			EventID:         f.Event.ID,
			BookingID:       initial.ID,
			ExpectedVersion: initial.Version,
		}
		proposal, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, request.Operation, request.Cancel)
		require.NoError(t, err)
		request.Proposal = proposal.ID
	}
	dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	application := "zinusya_massage_crash_" + uuid.NewString()
	query := dsn.Query()
	query.Set("application_name", application)
	dsn.RawQuery = query.Encode()
	request.DSN = dsn.String()
	releaseFault := func() {}
	if boundary == "before_commit" {
		releaseFault = blockMassageReceipt(t, f, request.Operation)
	}
	child, committed := startMassageCrashChild(t, request)
	if boundary == "before_commit" {
		require.Eventually(t, func() bool {
			var blocked bool
			err = f.H.store.Pool().QueryRow(t.Context(),
				"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event='advisory')", application).
				Scan(&blocked)
			return err == nil && blocked
		}, 10*time.Second, 20*time.Millisecond)
	} else {
		select {
		case line := <-committed:
			require.Equal(t, "massage committed", line)
		case <-time.After(10 * time.Second):
			t.Fatal("child did not commit massage operation")
		}
	}
	require.NoError(t, child.Process.Kill())
	require.Error(t, child.Wait())
	require.False(t, child.ProcessState.Success())
	// Let PostgreSQL finish the blocked statement and observe the dead connection.
	// The killed client cannot send COMMIT for this transaction.
	releaseFault()
	require.Eventually(t, func() bool {
		var count int
		readErr := f.H.store.Pool().QueryRow(t.Context(),
			"SELECT count(*) FROM pg_stat_activity WHERE application_name=$1", application).Scan(&count)
		return readErr == nil && count == 0
	}, 5*time.Second, 20*time.Millisecond)

	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	receipt, receiptErr := reopened.OwnMassageOperation(t.Context(), owner, request.Operation)
	if boundary == "before_commit" {
		require.ErrorIs(t, receiptErr, pgx.ErrNoRows)
		page, readErr := reopened.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 20)
		require.NoError(t, readErr)
		if kind == "book" {
			require.Empty(t, page.Bookings, "The uncommitted booking must roll back")
		} else {
			require.Equal(t, []db.MassageBooking{initial}, page.Bookings, "The uncommitted cancellation must roll back")
		}
		// Remove the fault before replaying exactly the original operation/proposal.
		_, err = reopened.Pool().Exec(t.Context(), "DROP TRIGGER block_massage_receipt ON shuttle_massage_operations")
		require.NoError(t, err)
	} else {
		require.NoError(t, receiptErr)
	}
	recovered := request.apply(t, reopened)
	if boundary == "after_commit" {
		require.Equal(t, receipt, recovered, "A lost caller result must recover the original committed receipt")
	}
	require.Equal(t, recovered, request.apply(t, reopened))
	current, err := reopened.OwnMassageBooking(t.Context(), owner, f.Event.ID, recovered.ID)
	require.NoError(t, err)
	require.Equal(t, recovered, current)
	var count int
	require.NoError(t, reopened.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_massage_operations WHERE operation_id=$1", request.Operation).Scan(&count))
	require.Equal(t, 1, count)
	page, err := reopened.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 1, "Recovery must never create another booking")
	require.Equal(t, map[string]string{"book": "booked", "cancel": "cancelled"}[kind], current.State)
}

func startMassageCrashChild(t *testing.T, request massageCrashRequest) (*exec.Cmd, <-chan string) {
	t.Helper()
	body, err := json.Marshal(request)
	require.NoError(t, err)
	child := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestMassageProcessCrashRollsBackOrRecoversCommittedOperation$", "-test.timeout=30s")
	child.WaitDelay = time.Second
	child.Env = append(os.Environ(), "ZINUSYA_MASSAGE_CRASH_CHILD=1", "ZINUSYA_MASSAGE_CRASH_REQUEST="+string(body))
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	committed := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			committed <- scanner.Text()
		}
	}()
	return child, committed
}

// A test-only trigger stalls receipt insertion after business writes, before commit.
func blockMassageReceipt(t *testing.T, f paymentFixture, operation uuid.UUID) func() {
	t.Helper()
	conn, err := f.H.store.Pool().Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	_, err = conn.Exec(t.Context(), "SELECT pg_advisory_lock(hashtextextended($1,0))", operation.String())
	require.NoError(t, err)
	var once sync.Once
	unlock := func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, unlockErr := conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtextextended($1,0))", operation.String())
			require.NoError(t, unlockErr)
		})
	}
	t.Cleanup(unlock)
	_, err = f.H.store.Pool().Exec(t.Context(), fmt.Sprintf(`
 CREATE FUNCTION stall_massage_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF NEW.operation_id='%s'::uuid THEN PERFORM pg_advisory_xact_lock(hashtextextended(NEW.operation_id::text,0)); END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER block_massage_receipt BEFORE INSERT ON shuttle_massage_operations
 FOR EACH ROW EXECUTE FUNCTION stall_massage_receipt();`, operation))
	require.NoError(t, err)
	return unlock
}
