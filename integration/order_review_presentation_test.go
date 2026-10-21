package integration_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func TestOrderEvidenceLegacyEmbeddedConsentRemainsReadable(t *testing.T) {
	t.Parallel()
	f, owner, order := orderEvidenceFixture(t)
	ctx := t.Context()
	proof, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, uuid.New(),
		orderEvidenceArgs(t, f, order, 79110, "legacy-review-presentation"))
	require.NoError(t, err)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	op := uuid.New()
	args := db.OrderEvidenceReview{EventID: f.Event.ID, EvidenceID: proof.ID,
		ExpectedVersion: 1, Decision: db.OrderEvidenceConfirmed, Reason: "Review legacy consent"}
	prepared, err := f.H.store.PrepareOrderEvidenceReview(ctx, scope, op, args)
	require.NoError(t, err)
	// Reproduce an unchanged pre-migration row, whose JSON embeds the full text.
	raw, err := json.Marshal(prepared)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_order_evidence_preparations SET document=$2,presentation='' WHERE id=$1",
		prepared.ID, raw)
	require.NoError(t, err)
	replay, err := f.H.store.PrepareOrderEvidenceReview(ctx, scope, op, args)
	require.NoError(t, err)
	require.Equal(t, prepared, replay)
	accepted, err := f.H.store.ReviewOrderEvidence(ctx, scope, op, prepared.ID, args)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidenceConfirmed, accepted.State)
}

func TestOrderEvidenceRollbackPreservesSeparateConsent(t *testing.T) {
	t.Parallel()
	f, owner, order := orderEvidenceFixture(t)
	ctx := t.Context()
	proof, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, uuid.New(),
		orderEvidenceArgs(t, f, order, 79111, "rollback-review-presentation"))
	require.NoError(t, err)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	op := uuid.New()
	args := db.OrderEvidenceReview{EventID: f.Event.ID, EvidenceID: proof.ID,
		ExpectedVersion: 1, Decision: db.OrderEvidenceConfirmed, Reason: "Preserve full consent"}
	prepared, err := f.H.store.PrepareOrderEvidenceReview(ctx, scope, op, args)
	require.NoError(t, err)
	raw, err := migrations.Files.ReadFile("00069_meal_order_evidence.sql")
	require.NoError(t, err)
	parts := strings.Split(string(raw), "-- +goose Down")
	require.Len(t, parts, 2)
	tx, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, parts[1])
	require.ErrorContains(t, err, "Order review presentations must be exported before rollback")
	require.NoError(t, tx.Rollback(ctx))
	replay, err := f.H.store.PrepareOrderEvidenceReview(ctx, scope, op, args)
	require.NoError(t, err)
	require.Equal(t, prepared, replay)
}
