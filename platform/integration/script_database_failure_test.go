package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestScriptCaughtSQLFailureStopsRuntimeAndKeepsInbox(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{InputJSON: "null", Code: `
for (let attempt = 0; attempt < 2; attempt++) {
  try {
    tools.orders.update({name:"create",choice:{customer:"Daniel",extras:{preparty:0}}});
  } catch (_) {}
}
return {continued:true};`}},
		{View: agent.OrdersView, Text: "Must not reach this reply"},
	}}
	f.b.Model = model
	_, err := f.db.Exec(t.Context(), `CREATE SEQUENCE core.script_sql_attempts;
CREATE FUNCTION core.reject_script_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM nextval('core.script_sql_attempts');
RAISE EXCEPTION 'private script SQL diagnostic'; END $$;
CREATE TRIGGER reject_script_order BEFORE INSERT ON core.orders
FOR EACH ROW EXECUTE FUNCTION core.reject_script_order()`)
	require.NoError(t, err)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "Create my preparty order"})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	runErr := f.b.Run(ctx)
	var attempts, pending, orders int
	var attempted bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT last_value,is_called,
(SELECT count(*) FROM bot.telegram_inbox), (SELECT count(*) FROM core.orders)
FROM core.script_sql_attempts`).Scan(&attempts, &attempted, &pending, &orders))
	t.Logf("SQL attempts=%d, pending inbox=%d, orders=%d, model calls=%d", attempts, pending, orders, len(model.inputs))
	require.True(t, attempted, "the script must reach the actual SQL fault")
	require.ErrorIs(t, runErr, core.ErrDatabase)
	require.NoError(t, ctx.Err(), "SQL failure must terminate before the caller cancels")
	require.Equal(t, 1, attempts, "catching a SQL failure must not permit another tool effect")
	require.Equal(t, 1, pending, "the failed input must remain durable for restart")
	require.Zero(t, orders)
	require.Len(t, model.inputs, 1, "a database failure must not become model-visible script completion")
}
