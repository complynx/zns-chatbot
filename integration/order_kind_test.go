package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func orderKindFingerprint(t *testing.T, f paymentFixture) string {
	t.Helper()
	var value string
	err := f.H.store.Pool().QueryRow(t.Context(), "SELECT jsonb_build_object(\n 'preparations',(SELECT "+
		"jsonb_agg(to_jsonb(p)-'order_kind' ORDER BY id) FROM shuttle_order_preparations p),\n 'orders',(SELECT "+
		"jsonb_agg(to_jsonb(o)-'order_kind' ORDER BY id) FROM shuttle_orders o),\n 'operations',(SELECT "+
		"jsonb_agg(to_jsonb(op)-'order_kind' ORDER BY operation_id,kind) FROM shuttle_order_operations op),\n "+
		"'evidence',(SELECT jsonb_agg(to_jsonb(ev)-'order_kind' ORDER BY id) FROM shuttle_order_evidence "+
		"ev))::text").Scan(&value)
	require.NoError(t, err)
	return value
}

// Synthetic future identities test isolation only, not a meal submission flow.
func seedOrderKindPreparation(t *testing.T, f paymentFixture, service uuid.UUID, kind db.OrderKind) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.H.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_order_preparations
 (id,user_id,event_id,source_draft_version,request_hash,arguments,snapshot,created_at,expires_at,order_kind)
 SELECT $1,user_id,event_id,source_draft_version,request_hash,arguments,'{"unsupported_meal_fixture":true}'::jsonb,
 clock_timestamp(),clock_timestamp()+interval '5 minutes',$3
 FROM shuttle_order_preparations WHERE id=$2`, id, service, string(kind))
	require.NoError(t, err)
	return id
}

func seedMealOrder(t *testing.T, f paymentFixture, preparation, service uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.H.store.Pool().Exec(t.Context(), "INSERT INTO shuttle_orders\n (id,user_id,event_id,"+
		"source_draft_version,preparation_id,request_hash,version,state,submitted_result,sales_closes_at,"+
		"submitted_at,order_kind)\n SELECT $1,user_id,event_id,source_draft_version,$2,request_hash,1,'submitted',"+
		"\n '{\"unsupported_meal_fixture\":true}'::jsonb,sales_closes_at,submitted_at,'meal'\n FROM "+
		"shuttle_orders WHERE id=$3", id, preparation, service)
	require.NoError(t, err)
	return id
}

func seedMealOperation(t *testing.T, f paymentFixture, preparation, serviceOperation uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.H.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_order_operations
 (user_id,operation_id,kind,event_id,preparation_id,business_hash,request_hash,result,source,order_kind)
 SELECT user_id,$1,'prepare',event_id,$2,business_hash,request_hash,
 '{"unsupported_meal_fixture":true}'::jsonb,'agent','meal'
 FROM shuttle_order_operations WHERE operation_id=$3 AND kind='prepare'`, id, preparation, serviceOperation)
	require.NoError(t, err)
	return id
}

func TestOrderKindServiceReceiptsRemainImmutableAndDefaultToService(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, _, args := submissionFixture(t)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	submitted, err := f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	evidenceArgs := orderEvidenceArgs(t, f, submitted.Order, 70001, "kind-service-proof")
	evidenceOp := uuid.New()
	evidence, err := f.H.store.SubmitOwnOrderEvidence(ctx, scope, evidenceOp, evidenceArgs)
	require.NoError(t, err)
	before := orderKindFingerprint(t, f)
	replay, err := f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, submitted, replay)
	prior, err := f.H.store.OwnOrderOperationByID(ctx, scope, op, db.OrderSubmit)
	require.NoError(t, err)
	require.NotEmpty(t, prior.Result)
	evidenceReplay, err := f.H.store.SubmitOwnOrderEvidence(ctx, scope, evidenceOp, evidenceArgs)
	require.NoError(t, err)
	require.Equal(t, evidence, evidenceReplay)
	require.Equal(t, before, orderKindFingerprint(t, f), "Replays preserve snapshots, receipts, hashes and evidence")
	// Old binaries still omit order_kind after expansion; the compatibility default is service.
	legacy := uuid.New()
	_, err = f.H.store.Pool().Exec(ctx, `INSERT INTO shuttle_order_preparations
 (id,user_id,event_id,source_draft_version,request_hash,arguments,snapshot,created_at,expires_at)
 SELECT $1,user_id,event_id,source_draft_version+10,request_hash,arguments,snapshot,created_at,expires_at
 FROM shuttle_order_preparations WHERE id=$2`, legacy, prepared.ID)
	require.NoError(t, err)
	var kind string
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT order_kind FROM shuttle_order_preparations WHERE id=$1", legacy).
			Scan(&kind),
	)
	require.Equal(t, string(db.OrderKindService), kind)
}

func TestOrderKindIndependentIdentityAndServiceAccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, _, args := submissionFixture(t)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	mealPreparation := seedOrderKindPreparation(t, f, prepared.ID, db.OrderKindMeal)
	alias, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, prepared, alias, "Newer meal preparation is not a service proposal")
	submitted, err := f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	meal := seedMealOrder(t, f, mealPreparation, submitted.Order.ID)
	mealOp := seedMealOperation(t, f, mealPreparation, op)
	page, err := f.H.store.ListOwnOrders(ctx, scope, args.EventID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Equal(t, []db.SubmittedOrder{submitted.Order}, page.Orders)
	_, err = f.H.store.OwnOrder(ctx, scope, args.EventID, meal)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.CancelOwnOrder(
		ctx,
		scope,
		uuid.New(),
		db.OrderCancellation{EventID: args.EventID, OrderID: meal, ExpectedVersion: 1},
		db.OrderSourceMiniApp,
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, mealPreparation, db.OrderSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnOrderOperationByID(ctx, scope, mealOp, db.OrderPrepare)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.PrepareOwnOrderSubmission(ctx, scope, mealOp, args, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict, "Operation UUID cannot be reused across business kinds")
	canonical, err := f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, submitted, canonical)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_orders WHERE user_id=$1 AND event_id=$2 AND "+
				"source_draft_version=$3", scope.UserID, args.EventID, args.ExpectedDraftVersion).
			Scan(&count),
	)
	require.Equal(t, 2, count)
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders WHERE order_kind='meal'").Scan(&count),
	)
	require.Equal(t, 1, count)
}

func requireOrderKindConstraint(t *testing.T, err error, name string) {
	t.Helper()
	var databaseError *pgconn.PgError
	require.ErrorAs(t, err, &databaseError)
	require.Equal(t, "23503", databaseError.Code)
	require.Equal(t, name, databaseError.ConstraintName)
}

func TestOrderKindForeignLinksAndPrematureMealEvidenceFailClosed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, _, args := submissionFixture(t)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	submitted, err := f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	mealPreparation := seedOrderKindPreparation(t, f, prepared.ID, db.OrderKindMeal)
	meal := seedMealOrder(t, f, mealPreparation, submitted.Order.ID)
	other := registrationEvent(t, f.H, f.Admin)
	unusedService := seedOrderKindPreparation(t, f, prepared.ID, db.OrderKindService)
	for _, test := range []struct {
		owner, event uuid.UUID
		kind         db.OrderKind
	}{
		{f.AmbassadorID, args.EventID, db.OrderKindService},
		{scope.UserID, other.ID, db.OrderKindService},
		{scope.UserID, args.EventID, db.OrderKindMeal},
	} {
		_, err = f.H.store.Pool().Exec(ctx, "INSERT INTO shuttle_orders\n (id,user_id,event_id,"+
			"source_draft_version,preparation_id,request_hash,version,state,submitted_result,sales_closes_at,"+
			"submitted_at,order_kind)\n SELECT $1,$2,$3,source_draft_version+10,$4,request_hash,1,'submitted',"+
			"submitted_result,sales_closes_at,submitted_at,$5\n FROM shuttle_orders WHERE id=$6", uuid.New(),
			test.owner, test.event, unusedService, string(test.kind), submitted.Order.ID)
		requireOrderKindConstraint(t, err, "order_preparation_kind")
	}
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_operations SET preparation_id=$1 WHERE operation_id=$2 AND kind='prepare'",
			mealPreparation, op)
	requireOrderKindConstraint(t, err, "order_operation_preparation_kind")
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_operations SET order_id=$1 WHERE operation_id=$2 AND kind='submit'", meal, op)
	requireOrderKindConstraint(t, err, "order_operation_order_kind")
	proof := orderEvidenceArgs(t, f, submitted.Order, 70002, "kind-meal-proof")
	proof.OrderID = meal
	_, err = f.H.store.SubmitOwnOrderEvidence(ctx, scope, uuid.New(), proof)
	file := proof.FileID
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().Exec(ctx, "INSERT INTO shuttle_order_evidence\n (id,order_id,event_id,user_id,"+
		"file_id,source_unique_id,reported_amount_minor,quoted_amount_minor,currency,currency_scale,sent_at,"+
		"submitted_at,due_at,admission_state,reason,state)\n VALUES($1,$2,$3,$4,$5,'synthetic-meal',2568,2568,"+
		"'BYN',2,clock_timestamp(),clock_timestamp(),clock_timestamp()+interval '1 hour','pending','','pending')",
		uuid.New(), meal, args.EventID, scope.UserID, file)
	requireOrderKindConstraint(t, err, "order_evidence_service_identity")
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_order_evidence").Scan(&count))
	require.Zero(t, count)
}
