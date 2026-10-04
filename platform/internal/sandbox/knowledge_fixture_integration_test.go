package sandbox_test

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

// This test owns a fresh role-test cluster, never an active acceptance stand.
func TestKnowledgeFixturePrivateRoleComposition(t *testing.T) {
	t.Parallel()
	if os.Getenv("KNOWLEDGE_FIXTURE_TEST_OWNER_URL") == "" {
		t.Skip("dedicated knowledge role-test cluster required")
	}
	owner := knowledgeFixturePool(t, "OWNER")
	operator := knowledgeFixturePool(t, "OPERATOR")
	ctx := t.Context()
	require.NoError(t, store.Migrate(ctx, owner))
	require.NoError(t, store.Seed(ctx, owner))
	require.NoError(t, sandbox.ApplyProductFixture(ctx, owner))
	_, err := sandbox.ApplyRegistrationFixture(ctx, owner, sandbox.RegistrationFixture{
		Stand: sandbox.RegistrationFixtureStand, Action: "init", OpensAt: time.Now().UTC()})
	require.NoError(t, err)
	roles, err := os.ReadFile("../../../docs/sandbox/fqa-stands/registration/runtime-roles.sql")
	require.NoError(t, err)
	_, err = owner.Exec(ctx, string(roles))
	require.NoError(t, err)
	_, err = owner.Exec(ctx, `GRANT SELECT ON core.events, core.knowledge_scopes TO zns_registration_operator;
 GRANT UPDATE(scope) ON core.knowledge_scopes TO zns_registration_operator;
 GRANT SELECT,INSERT,DELETE ON core.knowledge_permissions TO zns_registration_operator`)
	require.NoError(t, err)
	f := sandbox.KnowledgeFixture{Stand: sandbox.RegistrationFixtureStand,
		Action: "read", Scope: "sandbox-festival", Permission: "review"}
	_, err = sandbox.ApplyKnowledgeFixture(ctx, owner, f)
	require.Error(t, err, "owner is not the assigned private operator")
	for _, action := range []string{"revoke", "revoke", "grant", "grant"} {
		f.Action = action
		state, applyErr := sandbox.ApplyKnowledgeFixture(ctx, operator, f)
		require.NoError(t, applyErr)
		assert.Equal(t, action == "grant", state.Scope.CanReview)
		assert.True(t, state.Scope.CanCurate, "independent permission remains")
		assert.Equal(t, "bob", state.Actor)
		var unrelated bool
		require.NoError(t, owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.knowledge_permissions
 WHERE scope='sandbox-past' AND actor='bob' AND permission='review')`).Scan(&unrelated))
		assert.True(t, unrelated, "another event remains authorized")
	}
	assertKnowledgeRegistrationScopes(t, owner, operator)
	f.Action = "read"
	_, err = owner.Exec(ctx, `GRANT SELECT ON core.knowledge_facts TO zns_registration_operator`)
	require.NoError(t, err)
	_, err = sandbox.ApplyKnowledgeFixture(ctx, operator, f)
	require.ErrorContains(t, err, "privilege guard")
	_, err = owner.Exec(ctx, `REVOKE SELECT ON core.knowledge_facts FROM zns_registration_operator;
 UPDATE core.users SET telegram_id=404 WHERE id='bob'`)
	require.NoError(t, err)
	_, err = sandbox.ApplyKnowledgeFixture(ctx, operator, f)
	require.ErrorContains(t, err, "identity or marker")
}

func assertKnowledgeRegistrationScopes(t *testing.T, owner, operator *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	service := knowledge.Service{DB: owner}
	f := sandbox.KnowledgeFixture{Stand: sandbox.RegistrationFixtureStand, Permission: "review"}
	var err error
	for _, event := range []string{sandbox.RegistrationFixtureEventA, sandbox.RegistrationFixtureEventB} {
		f.Scope, f.Action = event, "grant"
		_, err = sandbox.ApplyKnowledgeFixture(ctx, operator, f)
		require.ErrorContains(t, err, "requested scope binding guard", "operator cannot provision missing destinations")
		_, err = service.Execute(ctx, "alice", knowledge.Command{
			Name: knowledge.Suggest, Key: "fixture-proposal-" + event,
			Event: event, Topic: "travel", FactKey: "meeting", Text: "Original event proposal",
		})
		require.NoError(t, err)
		for _, action := range []string{"grant", "revoke", "grant"} {
			f.Action = action
			state, applyErr := sandbox.ApplyKnowledgeFixture(ctx, operator, f)
			require.NoError(t, applyErr)
			assert.Equal(t, event, state.Scope.Event)
			assert.Equal(t, action == "grant", state.Scope.CanReview)
			assert.False(t, state.Scope.CanCurate, "review control does not grant curate")
			for _, actor := range []string{"alice", "visitor"} {
				scope, scopeErr := service.Scope(ctx, actor, event)
				require.NoError(t, scopeErr)
				assert.False(t, scope.CanReview, "control is fixed to bob")
				assert.False(t, scope.CanCurate)
			}
		}
	}
	var paymentA, paymentB, globalVisitor bool
	require.NoError(
		t,
		owner.QueryRow(
			ctx,
			`SELECT
 EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE owner='bob' AND event_id=$1),
 EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE owner='bob' AND event_id=$2),
 EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner='visitor')`,
			sandbox.RegistrationFixtureEventA,
			sandbox.RegistrationFixtureEventB,
		).Scan(&paymentA, &paymentB, &globalVisitor),
	)
	assert.True(t, paymentA, "knowledge control preserves event-A payment role")
	assert.False(t, paymentB, "knowledge control cannot assign payment role in B")
	assert.True(t, globalVisitor, "knowledge control preserves global booking category")
	f.Scope, f.Action = sandbox.RegistrationFixtureEventA, "revoke"
	_, err = sandbox.ApplyKnowledgeFixture(ctx, operator, f)
	require.NoError(t, err)
	otherScope, err := service.Scope(ctx, "bob", sandbox.RegistrationFixtureEventB)
	require.NoError(t, err)
	assert.True(t, otherScope.CanReview, "A withdrawal preserves B authority")
	f.Scope = "registration-fixture-c"
	_, err = sandbox.ApplyKnowledgeFixture(ctx, operator, f)
	require.ErrorContains(t, err, "scope guard")
}

func knowledgeFixturePool(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	db, err := pgxpool.New(t.Context(), os.Getenv("KNOWLEDGE_FIXTURE_TEST_"+role+"_URL"))
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, db.Ping(t.Context()))
	return db
}
