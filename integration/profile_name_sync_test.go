package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestProfileNameRiverWorkerRecoversAfterIAMOutageAndAppRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := newPeers(t)
	h := prepareHarness(t, p)
	userID, _ := registrationUser(t, h, 19105, false)
	scope := ownerProfileScope(t, h, userID)
	var mu sync.Mutex
	remote := map[string]any{"userId": scope.Subject, "state": "USER_STATE_ACTIVE",
		"details": map[string]any{"resourceOwner": "org"}, "human": map[string]any{
			"email":   map[string]any{"email": "tg+123+19105@telegram.invalid", "isVerified": true},
			"profile": map[string]any{"givenName": "Technical", "familyName": "Name"}}}
	down := true
	failed := make(chan struct{}, 1)
	bodies := make([]map[string]any, 0)
	original := p.iam.Config.Handler
	p.iam.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/users/"+scope.Subject {
			original.ServeHTTP(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if down {
			select {
			case failed <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodPatch {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			bodies = append(bodies, body)
			remote["human"].(map[string]any)["profile"] = body["human"].(map[string]any)["profile"]
			respond(w, map[string]any{})
			return
		}
		respond(w, map[string]any{"user": remote})
	})
	first := changeProfile(t, h, scope, personName(0, "Anna", "Old"))
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	t.Cleanup(func() {
		if t.Failed() {
			var state string
			err := h.store.Pool().
				QueryRow(ctx, `SELECT state::text FROM river_job WHERE kind='zinusya_profile_name_sync' ORDER BY id DESC LIMIT 1`).
				Scan(&state)
			t.Logf("profile worker state=%q query_error=%v", state, err)
		}
	})
	select {
	case <-failed:
	case <-time.After(10 * time.Second):
		t.Fatal("River worker did not reach IAM")
	}
	// Force the normal early River retry while the durable IAM backoff is
	// still active. It must snooze pending work rather than complete it.
	require.Eventually(t, func() bool {
		var state string
		err := h.store.Pool().
			QueryRow(ctx, `SELECT state::text FROM river_job WHERE kind='zinusya_profile_name_sync' ORDER BY id DESC LIMIT 1`).
			Scan(&state)
		return err == nil && (state == "retryable" || state == "scheduled")
	}, 10*time.Second, 20*time.Millisecond)
	_, updateErr := h.store.Pool().
		Exec(ctx, "UPDATE river_job SET state='available',scheduled_at=clock_timestamp() WH" +
"ERE kind='zinusya_profile_name_sync' AND state IN ('retryable','schedule" +
"d')")
	require.NoError(t, updateErr)
	require.Eventually(t, func() bool {
		var deferred bool
		err := h.store.Pool().QueryRow(ctx, "SELECT j.state='scheduled' AND j.scheduled_at>=n.next_attempt_at\nFROM ri" +
"ver_job j CROSS JOIN shuttle_profile_name_sync n WHERE j.kind='zinusya_p" +
"rofile_name_sync' ORDER BY j.id DESC LIMIT 1").Scan(&deferred)
		return err == nil && deferred
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	changeProfile(t, h, scope, personName(first.Version, "Anna", "New"))
	mu.Lock()
	down = false
	mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		var pending int
		err := h.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_profile_name_sync WHERE desired_version>synced_version`).
			Scan(&pending)
		return err == nil && pending == 0
	}, 10*time.Second, 20*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []map[string]any{{"human": map[string]any{"profile": map[string]any{
		"givenName": "Anna", "familyName": "New",
	}}}}, bodies)
}

func personName(version int64, first, last string) db.ProfileMutation {
	return db.ProfileMutation{OperationID: uuid.New(), Kind: "set_name", ExpectedVersion: version,
		FirstName: first, LastName: last}
}

func TestStructuredProfileNamePersistsLatestSyncAndImmutableReceipts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19101, false)
	scope := ownerProfileScope(t, h, userID)
	first := personName(0, "Анна Мария", "де ла Крус")
	saved := changeProfile(t, h, scope, first)
	require.Equal(t, "Анна Мария де ла Крус", saved.FullName)
	require.Equal(t, "user", saved.NameSource)
	ready, err := h.store.ReadyProfileNames(ctx, 123, "org")
	require.NoError(t, err)
	require.Len(t, ready, 1)
	require.Equal(t, saved.Version, ready[0].Version)
	language := profileChange("set_language", saved.Version)
	language.BotLanguage = "ru"
	saved = changeProfile(t, h, scope, language)
	unchanged := changeProfile(t, h, scope, personName(saved.Version, first.FirstName, first.LastName))
	require.Equal(t, saved.Version, unchanged.Version)
	latest := changeProfile(t, h, scope, personName(saved.Version, "Анна Мария", "Петрова"))

	// A reopened application connection recovers current desired work without
	// a queue job or the earlier caller remaining alive.
	reopened, err := db.Open(ctx, h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	calls := 0
	err = reopened.DispatchProfileName(ctx, scope, func(value db.ProfileNameSync) error {
		calls++
		require.Equal(t, latest.Version, value.Version)
		require.Equal(t, "Петрова", value.LastName)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	replayed := changeProfile(t, h, scope, first)
	require.Equal(t, "де ла Крус", replayed.LastName)
	ready, err = reopened.ReadyProfileNames(ctx, 123, "org")
	require.NoError(t, err)
	require.Empty(t, ready, "Historical receipt replay must not schedule an old name")
	err = reopened.DispatchProfileName(ctx, scope, func(db.ProfileNameSync) error { calls++; return nil })
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestStructuredProfileNameOutageRemainsPendingAndNewEditWins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19102, false)
	scope := ownerProfileScope(t, h, userID)
	old := changeProfile(t, h, scope, personName(0, "Anna", "Old"))
	unavailable := errors.New("fixture IAM unavailable")
	require.ErrorIs(t, h.store.DispatchProfileName(ctx, scope, func(db.ProfileNameSync) error {
		return unavailable
	}), unavailable)
	latest := changeProfile(t, h, scope, personName(old.Version, "Anna", "New"))
	calls := 0
	require.NoError(t, h.store.DispatchProfileName(ctx, scope, func(value db.ProfileNameSync) error {
		calls++
		require.Equal(t, latest.Version, value.Version)
		require.Equal(t, "New", value.LastName)
		return nil
	}))
	require.Equal(t, 1, calls)
}

func TestStructuredProfileNameRepairsLateOldWriteAfterNewNameWasAcknowledged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19106, false)
	scope := ownerProfileScope(t, h, userID)
	first := changeProfile(t, h, scope, personName(0, "Anna", "Old"))
	// The older request was admitted remotely but the local attempt was
	// cancelled, so even its local failure/backoff transaction cannot commit.
	attempt, cancel := context.WithCancel(ctx)
	require.Error(t, h.store.DispatchProfileName(attempt, scope, func(db.ProfileNameSync) error {
		cancel()
		return context.Canceled
	}))
	latest := changeProfile(t, h, scope, personName(first.Version, "Anna", "New"))
	remoteLastName := "Old"
	require.NoError(t, h.store.DispatchProfileName(ctx, scope, func(name db.ProfileNameSync) error {
		remoteLastName = name.LastName
		return nil
	}))
	require.Equal(t, latest.LastName, remoteLastName)
	remoteLastName = "Old" // IAM finally applies the older request after the newer read-back.
	_, err := h.store.Pool().
		Exec(ctx, "UPDATE shuttle_profile_name_sync SET next_attempt_at=clock_timestamp()-i" +
"nterval '1 second' WHERE user_id=$1", userID)
	require.NoError(t, err)
	ready, err := h.store.ReadyProfileNames(ctx, 123, "org")
	require.NoError(t, err)
	require.Len(t, ready, 1, "Acknowledged names still need periodic reconciliation after remote uncertainty")
	require.NoError(t, h.store.DispatchProfileName(ctx, scope, func(name db.ProfileNameSync) error {
		remoteLastName = name.LastName
		return nil
	}))
	require.Equal(t, latest.LastName, remoteLastName)
}

func TestStructuredProfileNameDispatchSerializesEditsAndChecksRevocation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19103, false)
	scope := ownerProfileScope(t, h, userID)
	old := changeProfile(t, h, scope, personName(0, "Anna", "Old"))
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- h.store.DispatchProfileName(ctx, scope, func(db.ProfileNameSync) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	edit := personName(old.Version, "Anna", "New")
	limited, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err := h.store.MutateProfile(limited, scope, edit)
	cancel()
	close(release)
	require.Error(t, err, "An edit must wait for the admitted older IAM dispatch")
	require.NoError(t, <-done)
	latest := changeProfile(t, h, scope, edit)
	require.NoError(t, h.store.RevokeUser(ctx, userID, true))
	called := false
	err = h.store.DispatchProfileName(ctx, scope, func(db.ProfileNameSync) error { called = true; return nil })
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.False(t, called)
	require.NoError(t, h.store.RevokeUser(ctx, userID, false))
	require.NoError(t, h.store.DispatchProfileName(ctx, scope, func(value db.ProfileNameSync) error {
		require.Equal(t, latest.LastName, value.LastName)
		return nil
	}))
}

func TestStructuredProfileNameQueueStorageFailureRollsBackNameAndReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	userID, _ := registrationUser(t, h, 19104, false)
	scope := ownerProfileScope(t, h, userID)
	_, err := h.store.Pool().Exec(ctx, `ALTER TABLE shuttle_profile_name_sync RENAME TO unavailable_name_sync`)
	require.NoError(t, err)
	edit := personName(0, "Anna", "Name")
	_, err = h.store.MutateProfile(ctx, scope, edit)
	require.Error(t, err)
	_, err = h.store.Pool().Exec(ctx, `ALTER TABLE unavailable_name_sync RENAME TO shuttle_profile_name_sync`)
	require.NoError(t, err)
	profile, err := h.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Zero(t, profile.Version)
	require.Empty(t, profile.FullName)
	_, err = h.store.ProfileOperation(ctx, scope, edit.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	changeProfile(t, h, scope, edit)
}
