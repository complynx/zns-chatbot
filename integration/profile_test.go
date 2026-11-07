package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func ownerProfileScope(t *testing.T, h *harness, userID uuid.UUID) db.ProfileScope {
	t.Helper()
	u, err := h.store.GetUser(context.Background(), userID)
	require.NoError(t, err)
	return db.ProfileScope{BotID: u.BotID, Organization: u.Organization, Subject: u.Subject, UserID: u.ID}
}

func changeProfile(t *testing.T, h *harness, scope db.ProfileScope, change db.ProfileMutation) db.UserProfile {
	t.Helper()
	result, err := h.store.MutateProfile(context.Background(), scope, change)
	require.NoError(t, err)
	return result
}

func profileChange(kind string, version int64) db.ProfileMutation {
	return db.ProfileMutation{OperationID: uuid.New(), Kind: kind, ExpectedVersion: version}
}

func expireProfileNameRequest(t *testing.T, h *harness, userID uuid.UUID) {
	t.Helper()
	// Shift both instants to preserve the duration invariant without waiting for a real deadline.
	_, err := h.store.Pool().Exec(context.Background(), `UPDATE shuttle_user_profiles
SET expectation_created_at=clock_timestamp()-interval '3 hours',
expectation_expires_at=clock_timestamp()-interval '1 hour' WHERE user_id=$1`, userID)
	require.NoError(t, err)
}

func TestProfileNameRequestCompletesWithLanguageChoiceAndDurableReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	userID, _ := registrationUser(t, h, 18101, false)
	scope := ownerProfileScope(t, h, userID)
	initial, err := h.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Zero(t, initial.Version)
	require.Empty(t, initial.FullName)
	require.Equal(t, "auto", initial.BotLanguage)
	request := profileChange("request_name", initial.Version)
	pending := changeProfile(t, h, scope, request)
	require.NotNil(t, pending.NameExpectation)
	require.Equal(t, 2*time.Hour, pending.NameExpectation.ExpiresAt.Sub(pending.NameExpectation.CreatedAt))
	repeated := changeProfile(t, h, scope, profileChange("request_name", pending.Version))
	require.Equal(t, pending.Version, repeated.Version)
	require.Equal(
		t,
		pending.NameExpectation,
		repeated.NameExpectation,
		"Repeating the question must not extend its deadline",
	)
	language := profileChange("set_language", pending.Version)
	language.BotLanguage = "ru"
	russian := changeProfile(t, h, scope, language)
	require.Equal(t, pending.NameExpectation, russian.NameExpectation)
	complete := profileChange("complete_name", pending.Version)
	complete.ExpectationID, complete.FullName = pending.NameExpectation.ID, "  Анна Иванова  "
	_, err = h.store.MutateProfile(ctx, scope, complete)
	require.ErrorIs(t, err, db.ErrConflict, "A concurrent language choice invalidates the old profile version")
	complete.ExpectedVersion = russian.Version
	completed := changeProfile(t, h, scope, complete)
	require.Equal(t, "Анна Иванова", completed.FullName)
	require.Equal(t, "ru", completed.BotLanguage)
	require.Nil(t, completed.NameExpectation)
	h.stop(t)
	h.start(t)
	replayed := changeProfile(t, h, scope, complete)
	require.Equal(t, completed, replayed)
	receipt, err := h.store.ProfileOperation(ctx, scope, complete.OperationID)
	require.NoError(t, err)
	require.Equal(t, completed, receipt)
	later := profileChange("set_name", completed.Version)
	later.FullName = "Анна Петрова"
	current := changeProfile(t, h, scope, later)
	require.Equal(t, "Анна Петрова", current.FullName)
	require.Equal(
		t,
		completed,
		changeProfile(t, h, scope, complete),
		"A replay returns the original result, not today's profile",
	)
}

func TestProfilePreferencesSurviveTelegramMetadataUpdates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	userID, _ := registrationUser(t, h, 18102, false)
	scope := ownerProfileScope(t, h, userID)
	name := profileChange("set_name", 0)
	name.FullName = "Saved Name"
	saved := changeProfile(t, h, scope, name)
	language := profileChange("set_language", saved.Version)
	language.BotLanguage = "en"
	saved = changeProfile(t, h, scope, language)
	h.stop(t)
	_, _, err := h.store.Intake(ctx, db.IntakeRequest{
		BotID: 123, UpdateID: 18102, TelegramUserID: 18102, ChatID: 18102,
		Profile: json.RawMessage(`{"first_name":"Different Telegram Name","language_code":"ru"}`),
		Payload: json.RawMessage(`{"kind":"message","text":"metadata update"}`),
	}, nil)
	require.NoError(t, err)
	current, err := h.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, saved.FullName, current.FullName)
	require.Equal(t, saved.BotLanguage, current.BotLanguage)
	require.Equal(t, saved.Version, current.Version)
	u, err := h.store.GetUser(ctx, userID)
	require.NoError(t, err)
	require.Contains(t, string(u.Profile), "Different Telegram Name")
	choice, err := h.store.ProfileLanguageByTelegram(ctx, 123, "org", 18102)
	require.NoError(t, err)
	require.Equal(t, "en", choice)
}

func TestExpiredNameRequestCannotCompleteOrChangeAnUnpaidPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPayment(t, newPeers(t))
	scope := ownerProfileScope(t, f.H, f.OwnerID)
	owner := paymentScope(t, f.H, f.OwnerID, false)
	before, err := f.H.store.Registration(ctx, owner.EventScope, f.Event.ID, f.OwnerID)
	require.NoError(t, err)
	request := profileChange("request_name", 0)
	pending := changeProfile(t, f.H, scope, request)
	expireProfileNameRequest(t, f.H, f.OwnerID)
	current, err := f.H.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Nil(t, current.NameExpectation)
	require.Equal(t, pending.Version, current.Version)
	complete := profileChange("complete_name", current.Version)
	complete.ExpectationID, complete.FullName = pending.NameExpectation.ID, "Too Late"
	_, err = f.H.store.MutateProfile(ctx, scope, complete)
	require.ErrorIs(t, err, db.ErrConflict)
	require.Equal(t, pending, changeProfile(t, f.H, scope, request), "Historical receipts survive deadline expiration")
	after, err := f.H.store.Registration(ctx, owner.EventScope, f.Event.ID, f.OwnerID)
	require.NoError(t, err)
	require.Equal(t, before, after, "Name expiry must not cancel registration or alter payment terms")
	renewed := changeProfile(t, f.H, scope, profileChange("request_name", current.Version))
	require.NotNil(t, renewed.NameExpectation)
	require.NotEqual(t, pending.NameExpectation.ID, renewed.NameExpectation.ID)
	require.Greater(t, renewed.Version, pending.Version)
}

func TestProfileCancellationAndExplicitNameRejectStaleOrCollidingOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	userID, _ := registrationUser(t, h, 18103, false)
	scope := ownerProfileScope(t, h, userID)
	request := profileChange("request_name", 0)
	pending := changeProfile(t, h, scope, request)
	cancel := profileChange("cancel_name", pending.Version)
	cancel.ExpectationID = uuid.New()
	_, err := h.store.MutateProfile(ctx, scope, cancel)
	require.ErrorIs(t, err, db.ErrConflict)
	cancel.ExpectationID = pending.NameExpectation.ID
	cancelled := changeProfile(t, h, scope, cancel)
	require.Nil(t, cancelled.NameExpectation)
	require.Empty(t, cancelled.FullName)
	collision := cancel
	collision.Kind, collision.ExpectationID, collision.FullName = "set_name", uuid.Nil, "Wrong Name"
	_, err = h.store.MutateProfile(ctx, scope, collision)
	require.ErrorIs(t, err, db.ErrConflict)
	pending = changeProfile(t, h, scope, profileChange("request_name", cancelled.Version))
	explicit := profileChange("set_name", pending.Version)
	explicit.FullName = "Explicit Name"
	saved := changeProfile(t, h, scope, explicit)
	require.Nil(t, saved.NameExpectation)
	require.Equal(t, explicit.FullName, saved.FullName)
	late := profileChange("complete_name", pending.Version)
	late.ExpectationID, late.FullName = pending.NameExpectation.ID, "Stale Name"
	_, err = h.store.MutateProfile(ctx, scope, late)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestProfileReceiptsRequireTheCurrentOwnerAndLiveAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	userID, _ := registrationUser(t, h, 18104, false)
	otherID, _ := registrationUser(t, h, 18105, false)
	scope := ownerProfileScope(t, h, userID)
	mutation := profileChange("set_name", 0)
	mutation.FullName = "Private Name"
	saved := changeProfile(t, h, scope, mutation)
	other := ownerProfileScope(t, h, otherID)
	_, err := h.store.ProfileOperation(ctx, other, mutation.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	for _, wrong := range []db.ProfileScope{
		{BotID: 124, Organization: scope.Organization, Subject: scope.Subject, UserID: userID},
		{BotID: scope.BotID, Organization: "another-org", Subject: scope.Subject, UserID: userID},
		{BotID: scope.BotID, Organization: scope.Organization, Subject: other.Subject, UserID: userID},
	} {
		_, err = h.store.GetProfile(ctx, wrong)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		_, err = h.store.MutateProfile(ctx, wrong, mutation)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		_, err = h.store.ProfileOperation(ctx, wrong, mutation.OperationID)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	_, err = h.store.SetUserRevoked(ctx, userID, "profile-fixture")
	require.NoError(t, err)
	_, err = h.store.MutateProfile(ctx, scope, mutation)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Even a committed replay requires live authorization")
	_, err = h.store.ProfileOperation(ctx, scope, mutation.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, "Private Name", saved.FullName)
}

func TestNameRequestUsesConfiguredTTLWithoutExtendingAnExistingRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	userID, _ := registrationUser(t, h, 18106, false)
	scope := ownerProfileScope(t, h, userID)
	settings, err := h.store.Settings(ctx)
	require.NoError(t, err)
	settings.Document.NameExpectationSeconds = 300
	settings, err = h.store.CompareAndSwapSettings(ctx, settings.Version, settings.Document, "profile-fixture")
	require.NoError(t, err)
	pending := changeProfile(t, h, scope, profileChange("request_name", 0))
	require.Equal(t, 5*time.Minute, pending.NameExpectation.ExpiresAt.Sub(pending.NameExpectation.CreatedAt))
	settings.Document.NameExpectationSeconds = 600
	_, err = h.store.CompareAndSwapSettings(ctx, settings.Version, settings.Document, "profile-fixture")
	require.NoError(t, err)
	unchanged := changeProfile(t, h, scope, profileChange("request_name", pending.Version))
	require.Equal(t, pending.NameExpectation, unchanged.NameExpectation)
	expireProfileNameRequest(t, h, userID)
	fresh := changeProfile(t, h, scope, profileChange("request_name", pending.Version))
	require.Equal(t, 10*time.Minute, fresh.NameExpectation.ExpiresAt.Sub(fresh.NameExpectation.CreatedAt))
}
