package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func allowExistingIAMNameImport(t *testing.T, h *harness, id uuid.UUID) {
	t.Helper()
	_, err := h.store.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_iam_name_origins(user_id,origin) VALUES($1,'existing')`, id)
	require.NoError(t, err)
}

func TestIAMNameOriginMigrationBackfillsUnknownAndProtectsNewProvenance(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	id, _ := registrationUser(t, h, 19240, false)
	scope := ownerProfileScope(t, h, id)
	legacy := profileChange("set_name", 0)
	legacy.FullName = "Retained legacy name"
	saved := changeProfile(t, h, scope, legacy)
	driver := stdlib.OpenDBFromPool(h.store.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(t.Context(), 72, false)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(t.Context(), 72, true)
	require.NoError(t, err)
	var origin string
	err = h.store.Pool().
		QueryRow(t.Context(), `SELECT origin FROM shuttle_iam_name_origins WHERE user_id=$1`, id).
		Scan(&origin)
	require.NoError(t, err)
	require.Equal(t, "unknown", origin)
	receipt, err := h.store.ProfileOperation(t.Context(), scope, legacy.OperationID)
	require.NoError(t, err)
	require.Equal(t, saved, receipt)
	newID := uuid.New()
	_, err = h.store.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile) VALUES($1,123,19241,'{}')`, newID)
	require.NoError(t, err)
	require.NoError(t, h.store.RecordIAMNameOrigin(t.Context(), newID, 123, "org", true))
	_, err = provider.ApplyVersion(t.Context(), 72, false)
	require.ErrorContains(t, err, "IAM name provenance must be exported")
	err = h.store.Pool().
		QueryRow(t.Context(), `SELECT origin FROM shuttle_iam_name_origins WHERE user_id=$1`, newID).
		Scan(&origin)
	require.NoError(t, err)
	require.Equal(t, "technical", origin)
}

func TestIAMNameImportReusesExistingNamesWithoutHoldingConnectionOrOverwritingLaterEdits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := prepareHarness(t, newPeers(t))
	id, _ := registrationUser(t, h, 19201, false)
	allowExistingIAMNameImport(t, h, id)
	scope := ownerProfileScope(t, h, id)
	dsn, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	single, err := db.Open(ctx, dsn.String())
	require.NoError(t, err)
	defer single.Close()
	calls := 0
	read := func(ctx context.Context) (string, string, error) {
		calls++
		bounded, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, readErr := single.GetProfile(bounded, scope)
		require.NoError(t, readErr, "IAM I/O must not retain the only database connection")
		return " Anna Maria ", " de la Cruz ", nil
	}
	imported, err := single.ImportIAMProfileName(ctx, scope, read)
	require.NoError(t, err)
	require.Equal(t, "Anna Maria de la Cruz", imported.FullName)
	require.Equal(t, "iam", imported.NameSource)
	require.Equal(t, int64(1), imported.Version)
	ready, err := single.ReadyProfileNames(ctx, 123, "org")
	require.NoError(t, err)
	require.Empty(t, ready, "Reading existing IAM names must not write them back")
	_, err = single.ImportIAMProfileName(ctx, scope, read)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	changed := changeProfile(t, h, scope, personName(imported.Version, "Anna Maria", "New"))
	latest, err := single.ImportIAMProfileName(ctx, scope, read)
	require.NoError(t, err)
	require.Equal(t, changed.Version, latest.Version)
	require.Equal(t, "New", latest.LastName)
	require.Equal(t, 1, calls)
}

func TestIAMNameImportCannotOverwriteEditOrRevocationDuringRead(t *testing.T) {
	t.Parallel()
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "local edit", true: "revocation"}[revoke], func(t *testing.T) {
			t.Parallel()
			h := prepareHarness(t, newPeers(t))
			id, _ := registrationUser(t, h, 19202, false)
			allowExistingIAMNameImport(t, h, id)
			scope := ownerProfileScope(t, h, id)
			profile, err := h.store.ImportIAMProfileName(
				t.Context(),
				scope,
				func(ctx context.Context) (string, string, error) {
					if revoke {
						require.NoError(t, h.store.RevokeUser(ctx, id, true))
					} else {
						_, changeErr := h.store.MutateProfile(ctx, scope, personName(0, "Explicit", "Name"))
						require.NoError(t, changeErr)
					}
					return "IAM", "Old", nil
				},
			)
			if revoke {
				require.Error(t, err)
				require.Empty(t, profile.FullName)
			} else {
				require.NoError(t, err)
				require.Equal(t, "Explicit Name", profile.FullName)
				require.Equal(t, "user", profile.NameSource)
			}
		})
	}
}

func TestIAMNameImportPreservesNameDialogStartedDuringRead(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	id, _ := registrationUser(t, h, 19260, false)
	allowExistingIAMNameImport(t, h, id)
	scope := ownerProfileScope(t, h, id)
	var pending db.UserProfile
	p, err := h.store.ImportIAMProfileName(t.Context(), scope, func(ctx context.Context) (string, string, error) {
		var changeErr error
		pending, changeErr = h.store.MutateProfile(ctx, scope, profileChange("request_name", 0))
		require.NoError(t, changeErr)
		return "Existing", "Name", nil
	})
	require.NoError(t, err)
	require.Equal(t, pending.Version, p.Version)
	require.Equal(t, pending.NameExpectation, p.NameExpectation)
	require.Empty(t, p.FullName)
	_, err = h.store.ImportIAMProfileName(t.Context(), scope, func(context.Context) (string, string, error) {
		t.Fatal("An active name dialog must not trigger an IAM import")
		return "", "", nil
	})
	require.NoError(t, err)
	complete := personName(p.Version, "Chosen", "Name")
	complete.Kind = "complete_name"
	complete.ExpectationID = p.NameExpectation.ID
	require.Equal(t, "Chosen Name", changeProfile(t, h, scope, complete).FullName)
}

func TestIAMNameImportExcludesTechnicalUnknownAndLegacyNames(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	for n, origin := range []string{"technical", "unknown", "legacy"} {
		id, _ := registrationUser(t, h, int64(19210+n), false)
		scope := ownerProfileScope(t, h, id)
		if origin == "legacy" {
			legacy := profileChange("set_name", 0)
			legacy.FullName = "Older user-provided name"
			changeProfile(t, h, scope, legacy)
		} else {
			_, err := h.store.Pool().
				Exec(t.Context(), `INSERT INTO shuttle_iam_name_origins(user_id,origin) VALUES($1,$2)`, id, origin)
			require.NoError(t, err)
		}
		called := false
		p, err := h.store.ImportIAMProfileName(
			t.Context(),
			scope,
			func(context.Context) (string, string, error) { called = true; return "Technical", "Name", nil },
		)
		require.NoError(t, err)
		require.False(t, called)
		require.Empty(t, p.FirstName)
		require.Empty(t, p.NameSource)
	}
}

func TestIAMNameImportRetriesUnavailableReadAndLeavesIncompleteNamesForOnboarding(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	id, _ := registrationUser(t, h, 19220, false)
	allowExistingIAMNameImport(t, h, id)
	scope := ownerProfileScope(t, h, id)
	unavailable := errors.New("fixture IAM read unavailable")
	_, err := h.store.ImportIAMProfileName(
		t.Context(),
		scope,
		func(context.Context) (string, string, error) { return "", "", unavailable },
	)
	require.ErrorIs(t, err, unavailable)
	p, err := h.store.ImportIAMProfileName(
		t.Context(),
		scope,
		func(context.Context) (string, string, error) { return "Only given name", "", nil },
	)
	require.NoError(t, err)
	require.Empty(t, p.FullName)
	require.Zero(t, p.Version)
}

func TestIAMNameImportLeavesKnownProvisioningFallbacksForExplicitOnboarding(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	for index, names := range [][2]string{{"Telegram user", "Person"}, {"Telegram", "Person"}, {"Given", "Given"}} {
		id, _ := registrationUser(t, h, int64(19250+index), false)
		allowExistingIAMNameImport(t, h, id)
		p, err := h.store.ImportIAMProfileName(
			t.Context(),
			ownerProfileScope(t, h, id),
			func(context.Context) (string, string, error) { return names[0], names[1], nil },
		)
		require.NoError(t, err)
		require.Empty(t, p.FullName)
		require.Empty(t, p.NameSource)
	}
}

func TestMiniAppNameImportReusesExistingIAMProfileButNotProvisionedTelegramNames(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "new technical identity", true: "existing IAM identity"}[existing],
			func(t *testing.T) {
				t.Parallel()
				p := newPeers(t)
				p.miniAppOrigin = appTestOrigin
				if existing {
					p.users["existing-iam-owner"] = map[string]any{
						"userId":  "existing-iam-owner",
						"state":   "USER_STATE_ACTIVE",
						"details": map[string]any{"resourceOwner": "org"},
						"human": map[string]any{
							"profile": map[string]any{"givenName": "Anna Maria", "familyName": "de la Cruz"},
							"email":   map[string]any{"email": "tg+123+456@telegram.invalid", "isVerified": true}},
					}
				}
				h := setup(t, p)
				code, raw, _ := appCall(
					t,
					h,
					http.MethodPost,
					appBootstrapPath,
					appBody(appProof(456), 92),
					"",
					appTestOrigin,
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var session appSession
				require.NoError(t, json.Unmarshal(raw, &session))
				profile, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, session.UserID))
				require.NoError(t, err)
				if existing {
					require.Equal(t, "Anna Maria de la Cruz", profile.FullName)
					require.Equal(t, "iam", profile.NameSource)
				} else {
					require.Empty(t, profile.FullName)
					require.Empty(t, profile.NameSource)
					var origin string
					require.NoError(
						t,
						h.store.Pool().
							QueryRow(t.Context(), `SELECT origin FROM shuttle_iam_name_origins WHERE user_id=$1`, session.UserID).
							Scan(&origin),
					)
					require.Equal(t, "technical", origin)
				}
				p.mu.Lock()
				require.Empty(t, p.runs)
				p.mu.Unlock()
			},
		)
	}
}

func TestIAMNameProvisioningIntentSurvivesReopenAndBlocksImport(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	id := uuid.New()
	_, err := h.store.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile) VALUES($1,123,19230,'{}')`, id)
	require.NoError(t, err)
	require.NoError(t, h.store.RecordIAMNameOrigin(t.Context(), id, 123, "org", true))
	// A lookup that recovers the remotely created user must not downgrade the
	// technical origin to user-supplied names after a lost create response.
	require.NoError(t, h.store.RecordIAMNameOrigin(t.Context(), id, 123, "org", false))
	_, err = h.store.BindSubject(t.Context(), id, "created-before-lost-response", "org")
	require.NoError(t, err)
	reopened, err := db.Open(t.Context(), h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	called := false
	profile, err := reopened.ImportIAMProfileName(
		t.Context(),
		ownerProfileScope(t, h, id),
		func(context.Context) (string, string, error) { called = true; return "Telegram", "Hints", nil },
	)
	require.NoError(t, err)
	require.False(t, called)
	require.Empty(t, profile.FullName)
}
