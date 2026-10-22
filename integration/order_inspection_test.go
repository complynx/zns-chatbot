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
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func inspectOrderHTTP(t *testing.T, f paymentFixture, operation uuid.UUID, expected int) inspectedKnowledgeOperation {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, "/internal/agent/operations/inspect", map[string]any{
		"protocol_version": 1, "operation_id": operation, "module": "zinusya_orders", "method": "submit",
	})
	require.Equal(t, expected, code, string(raw))
	var found inspectedKnowledgeOperation
	if expected == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &found))
	}
	return found
}

func TestOrderInspectorContinuesOriginalOperationAcrossBothTransports(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"native", "agent"} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			f, _, session, draft := ordersAppFixture(t)
			args := saveOrderAppDraft(t, f, session, draft)
			op := uuid.New()
			var prepared db.OrderPreparation
			if origin == "native" {
				prepared = prepareOrderApp(t, f, session, op, args)
			} else {
				wire := prepareOrderWire(t, f, op, args)
				prepared = db.OrderPreparation{
					ID:          wire.Preconditions.ProposalID,
					Arguments:   wire.Arguments,
					Description: wire.Description,
					ExpiresAt:   wire.ExpiresAt,
				}
			}
			inspected := inspectOrderHTTP(t, f, op, http.StatusOK)
			require.Equal(t, "prepared", inspected.Status)
			require.Equal(t, op.String(), inspected.OperationID)
			require.Equal(t, prepared.ID, inspected.Preconditions.ProposalID)
			require.True(t, prepared.ExpiresAt.Equal(inspected.ExpiresAt))
			var recoveredArgs db.OrderSubmission
			require.NoError(t, json.Unmarshal(inspected.Arguments, &recoveredArgs))
			require.Equal(t, args, recoveredArgs)
			code, raw, _ := appCall(
				t,
				f.H,
				http.MethodGet,
				ordersAppPath(args.EventID)+"/operation-receipts/prepare/"+op.String(),
				"",
				session.Token,
				"",
			)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.NotContains(t, string(raw), "operation_contract")
			var effect db.OrderSubmissionResult
			if origin == "native" {
				code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", map[string]any{
					"protocol_version": 1,
					"operation_id":     op,
					"idempotency_key":  op,
					"arguments":        args,
					"confirmation": map[string]any{
						"mode":          "exact_parameters",
						"preconditions": map[string]any{"proposal_id": prepared.ID},
					},
				})
				require.Equal(t, http.StatusOK, code, string(raw))
				var result orderWireResult[db.OrderSubmissionResult]
				require.NoError(t, json.Unmarshal(raw, &result))
				require.NotNil(t, result.Result)
				effect = *result.Result
			} else {
				code, raw, _ = appCall(
					t,
					f.H,
					http.MethodPost,
					ordersAppPath(args.EventID)+"/submit",
					paymentAppJSON(t, orderAppSubmitBody(op, prepared)),
					session.Token,
					appTestOrigin,
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				require.NoError(t, json.Unmarshal(raw, &effect))
			}
			completed := inspectOrderHTTP(t, f, op, http.StatusOK)
			require.Equal(t, "succeeded", completed.Status)
			require.Equal(t, inspected.Preconditions, completed.Preconditions)
			require.Equal(t, inspected.ExpiresAt, completed.ExpiresAt)
			var receipt db.OrderSubmissionResult
			require.NoError(t, json.Unmarshal(completed.Result, &receipt))
			require.Equal(t, effect, receipt)
			// Reopening the form after completion must recover the same original effect.
			code, replay, _ := appCall(
				t,
				f.H,
				http.MethodPost,
				ordersAppPath(args.EventID)+"/submit",
				paymentAppJSON(t, orderAppSubmitBody(op, prepared)),
				session.Token,
				appTestOrigin,
			)
			require.Equal(t, http.StatusOK, code, string(replay))
			require.NoError(t, json.Unmarshal(replay, &receipt))
			require.Equal(t, effect, receipt)
			scope := ownerProfileScope(t, f.H, f.OwnerID)
			_, err := f.H.store.CancelOwnOrder(
				t.Context(),
				scope,
				uuid.New(),
				db.OrderCancellation{
					EventID:         args.EventID,
					OrderID:         effect.Order.ID,
					ExpectedVersion: effect.Order.Version,
				},
				db.OrderSourceAgent,
			)
			require.NoError(t, err)
			after := inspectOrderHTTP(t, f, op, http.StatusOK)
			require.JSONEq(t, string(completed.Result), string(after.Result))
			var count int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&count),
			)
			require.Equal(t, 1, count)
		})
	}
}

func TestOrderInspectorKeepsPerOperationTermsAndRejectsLegacyAndChangedBinding(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	contract := db.OrderPreparationContract{Issuer: f.H.peers.iam.URL, Actor: "actor", Revision: "mvp-91"}
	firstOp, secondOp, legacyOp := uuid.New(), uuid.New(), uuid.New()
	prepare := func(op uuid.UUID, terms string) db.OrderPreparation {
		t.Helper()
		p, err := f.H.store.PrepareOwnOrderSubmissionWithContract(
			ctx,
			scope,
			op,
			args,
			db.OrderSourceAgent,
			contract,
			func(db.OrderPreparation) (string, error) { return terms, nil },
		)
		require.NoError(t, err)
		return p
	}
	first := prepare(firstOp, "Original English terms")
	second := prepare(secondOp, "Original Russian terms")
	require.Equal(t, first.ID, second.ID, "Different operations may share the same proposal")
	require.Equal(t, first, prepare(firstOp, "Changed terms must not replace consent"))
	for op, terms := range map[uuid.UUID]string{firstOp: first.Description, secondOp: second.Description} {
		found, err := f.H.store.InspectOrderSubmissionOperation(ctx, scope, op, contract)
		require.NoError(t, err)
		require.Equal(t, terms, found.Preparation.Description)
	}
	_, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, legacyOp, args, db.OrderSourceAgent)
	require.NoError(t, err)
	legacy := prepare(legacyOp, "Do not retrofit this marker")
	require.Empty(t, legacy.Description)
	_, err = f.H.store.InspectOrderSubmissionOperation(ctx, scope, legacyOp, contract)
	require.ErrorIs(t, err, db.ErrConflict)
	for _, changed := range []db.OrderPreparationContract{
		{Issuer: "other", Actor: contract.Actor, Revision: contract.Revision},
		{Issuer: contract.Issuer, Actor: "other", Revision: contract.Revision},
		{Issuer: contract.Issuer, Actor: contract.Actor, Revision: "other"},
	} {
		_, err = f.H.store.InspectOrderSubmissionOperation(ctx, scope, firstOp, changed)
		require.ErrorIs(t, err, db.ErrConflict)
	}
	foreign := ownerProfileScope(t, f.H, f.AmbassadorID)
	_, err = f.H.store.InspectOrderSubmissionOperation(ctx, foreign, firstOp, contract)
	require.Error(t, err)
	// A matching canonical order from another operation is not the original receipt.
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, firstOp, args, first.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	other, err := f.H.store.InspectOrderSubmissionOperation(ctx, scope, secondOp, contract)
	require.NoError(t, err)
	require.Nil(t, other.Result)
}

func TestOrderContractFormattingFailureRollsBackAndSingleConnectionDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	contract := db.OrderPreparationContract{Issuer: "issuer", Actor: "actor", Revision: "revision"}
	dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	reopened, err := db.Open(ctx, dsn.String())
	require.NoError(t, err)
	defer reopened.Close()
	require.Equal(t, int32(1), reopened.Pool().Config().MaxConns)
	op := uuid.New()
	sentinel := errors.New("cannot format original terms")
	_, err = reopened.PrepareOwnOrderSubmissionWithContract(
		ctx,
		scope,
		op,
		args,
		db.OrderSourceAgent,
		contract,
		func(db.OrderPreparation) (string, error) { return "", sentinel },
	)
	require.ErrorIs(t, err, sentinel)
	var preparations, receipts int
	require.NoError(
		t,
		reopened.Pool().
			QueryRow(ctx, "SELECT (SELECT count(*) FROM shuttle_order_preparations),(SELECT count(*) FROM shuttle_order_operations)").
			Scan(&preparations, &receipts),
	)
	require.Zero(t, preparations)
	require.Zero(t, receipts)
	p, err := reopened.PrepareOwnOrderSubmissionWithContract(
		ctx,
		scope,
		op,
		args,
		db.OrderSourceAgent,
		contract,
		func(db.OrderPreparation) (string, error) { return "Review this order", nil },
	)
	require.NoError(t, err)
	found, err := reopened.InspectOrderSubmissionOperation(ctx, scope, op, contract)
	require.NoError(t, err)
	require.Equal(t, p, found.Preparation)
}

func TestOrderInspectorReturnsExpiredTermsWithoutRenewingAndRechecksCurrentAccess(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	op := uuid.New()
	prepared := prepareOrderWire(t, f, op, args)
	// Advance only this fixture's recorded deadline; do not change the host clock.
	expired := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	_, err := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_preparations SET created_at=$2::timestamptz-interval '10 seconds',expires_at=$2 WHERE id=$1", prepared.Preconditions.ProposalID, expired)
	require.NoError(t, err)
	expiryJSON, err := json.Marshal(expired)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_operations SET result=jsonb_set(result,'{expires_at}',$2::jsonb) WHERE operation_id=$1 AND kind='prepare'", op, string(expiryJSON))
	require.NoError(t, err)
	found := inspectOrderHTTP(t, f, op, http.StatusOK)
	require.True(t, expired.Equal(found.ExpiresAt))
	require.Equal(t, "prepared", found.Status)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.Preconditions.ProposalID, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	inspectOrderHTTP(t, f, op, http.StatusNotFound)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
}

func TestOrderInspectorAndPrepareReplayKeepOriginalLanguage(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	profile, err := f.H.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	profile, err = f.H.store.MutateProfile(
		ctx,
		scope,
		db.ProfileMutation{
			OperationID:     uuid.New(),
			ExpectedVersion: profile.Version,
			Kind:            "set_language",
			BotLanguage:     "en",
		},
	)
	require.NoError(t, err)
	op := uuid.New()
	original := prepareOrderWire(t, f, op, args)
	require.Contains(t, original.Description, "Submit order?")
	_, err = f.H.store.MutateProfile(
		ctx,
		scope,
		db.ProfileMutation{
			OperationID:     uuid.New(),
			ExpectedVersion: profile.Version,
			Kind:            "set_language",
			BotLanguage:     "ru",
		},
	)
	require.NoError(t, err)
	replay := prepareOrderWire(t, f, op, args)
	require.Equal(t, original, replay)
	code, raw := adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		"/internal/agent/operations/inspect",
		map[string]any{"protocol_version": 1, "operation_id": op, "module": "zinusya_orders", "method": "submit"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var inspected struct {
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(raw, &inspected))
	require.Equal(t, original.Description, inspected.Description)
}

func TestOrderInspectorRecoversCanonicalReceiptWithNewObservationTime(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	contract := db.OrderPreparationContract{Issuer: "issuer", Actor: "actor", Revision: "revision"}
	prepare := func(op uuid.UUID) db.OrderPreparation {
		t.Helper()
		p, err := f.H.store.PrepareOwnOrderSubmissionWithContract(
			ctx,
			scope,
			op,
			args,
			db.OrderSourceAgent,
			contract,
			func(db.OrderPreparation) (string, error) { return "Same order terms", nil },
		)
		require.NoError(t, err)
		return p
	}
	originalOp, newOp := uuid.New(), uuid.New()
	original := prepare(originalOp)
	_, err := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_preparations SET created_at=clock_timestamp()-interval '10 seconds',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", original.ID)
	require.NoError(t, err)
	fresh := prepare(newOp)
	require.NotEqual(t, original.ID, fresh.ID)
	require.False(t, original.Snapshot.Quote.ObservedAt.Equal(fresh.Snapshot.Quote.ObservedAt))
	canonical, err := f.H.store.SubmitOwnOrder(ctx, scope, newOp, args, fresh.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	alias, err := f.H.store.SubmitOwnOrder(ctx, scope, originalOp, args, original.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, canonical, alias)
	inspected, err := f.H.store.InspectOrderSubmissionOperation(ctx, scope, originalOp, contract)
	require.NoError(t, err)
	require.NotNil(t, inspected.Result)
	require.Equal(t, alias, *inspected.Result)
	require.Equal(t, original, inspected.Preparation)
}
