package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func startModernContinuation(t *testing.T, f *fixture, user int64, orderID, tool string) string {
	t.Helper()
	result := runModernContinuation(t, f, 42000, user, "Read complete order "+orderID, fmt.Sprintf(`
let p=tools.%s({order_id:%q});for(let i=1;i<8 && p.more;i++){p=tools.%s({order_id:%q,cursor:p.next_cursor});}return {next_cursor:p.next_cursor,more:p.more};`, tool, orderID, tool, orderID))
	var chunk core.ReadChunk
	require.NoError(t, json.Unmarshal(result, &chunk))
	require.True(t, chunk.More)
	require.NotEmpty(t, chunk.NextCursor)
	return chunk.NextCursor
}

func TestModernOrdersCompleteCursorAcrossTurns(t *testing.T) {
	t.Parallel()
	f, _, order := modernLargeOrder(t, false)
	cursor := startModernContinuation(t, f, identity.AliceTelegramID, order.ID, "orders.inspect")
	f.b = &bot.Bot{DB: f.db, API: f.b.API, TG: f.b.TG}
	result := runModernContinuation(t, f, 42001, identity.AliceTelegramID, "Continue reading "+order.ID, fmt.Sprintf(`
const p=tools.orders.inspect({order_id:%q,cursor:%q});return {offset:p.offset,more:p.more};`, order.ID, cursor))
	assert.JSONEq(t, `{"offset":32000,"more":true}`, string(result))
}

func TestModernOrdersCompleteContinuationScopesAndStale(t *testing.T) {
	t.Parallel()
	f, s, order := modernLargeOrder(t, false)
	cursor := startModernContinuation(t, f, identity.AliceTelegramID, order.ID, "orders.inspect")
	result := runModernContinuation(t, f, 42101, identity.BobTelegramID, "Continue reading "+order.ID, fmt.Sprintf(`
let denied=0;for(const args of [{order_id:%q,cursor:%q},{order_id:%q,resume:true}]){try{tools.orders.inspect(args);}catch(_){denied++;}}
try{tools.orders.review.read({order_id:%q,cursor:%q});}catch(_){denied++;}return {denied};`, order.ID, cursor, order.ID, order.ID, cursor))
	assert.JSONEq(t, `{"denied":3}`, string(result))
	result = runModernContinuation(t, f, 42102, identity.AliceTelegramID, "Continue reading "+order.ID, fmt.Sprintf(`
let denied=false;try{tools.orders.inspect({event:"different-event",order_id:%q,cursor:%q});}catch(_){denied=true;}return {denied};`, order.ID, cursor))
	assert.JSONEq(t, `{"denied":true}`, string(result))
	changed := orderCommand("edit", order)
	changed.Choice = &orders.ChoiceInput{Customer: "Changed manually"}
	_, err := s.Execute(t.Context(), "alice", changed)
	require.NoError(t, err)
	f.b = &bot.Bot{DB: f.db, API: f.b.API, TG: f.b.TG}
	result = runModernContinuation(t, f, 42103, identity.AliceTelegramID, "Continue and delete "+order.ID, fmt.Sprintf(`
const stale=tools.orders.inspect({order_id:%q,resume:true});let edited=false;try{tools.orders.update({name:"delete",order_id:%q});edited=true;}catch(_){}
return {stale,edited};`, order.ID, order.ID))
	assert.JSONEq(t, `{"stale":{"error":"stale","restart":true},"edited":false}`, string(result))
	current, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "Changed manually", current.Choice.Customer)
}

func TestModernOrdersCompleteReviewCurrentRightsAndAttempt(t *testing.T) {
	t.Parallel()
	f, s, order := modernLargeOrder(t, true)
	cash := orderCommand("cash", order)
	cash.PaymentAdmin = "bob"
	order, err := s.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	_ = startModernContinuation(t, f, identity.BobTelegramID, order.ID, "orders.review.read")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.order_admins WHERE owner='bob' AND event_id='sandbox-festival'`)
	require.NoError(t, err)
	result := runModernContinuation(t, f, 42201, identity.BobTelegramID, "Continue reviewing "+order.ID, fmt.Sprintf(`
let read=false;try{tools.orders.review.read({order_id:%q,resume:true});read=true;}catch(_){}return {read,listed:tools.$list().some(x=>x.name==="orders.review.read")};`, order.ID))
	assert.JSONEq(t, `{"read":false,"listed":false}`, string(result))
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.order_admins(event_id,owner,country) VALUES('sandbox-festival','bob','be')`,
	)
	require.NoError(t, err)
	result = runModernContinuation(
		t,
		f,
		42202,
		identity.BobTelegramID,
		"Continue reviewing "+order.ID,
		fmt.Sprintf(`return {more:tools.orders.review.read({order_id:%q,resume:true}).more};`, order.ID),
	)
	assert.JSONEq(t, `{"more":true}`, string(result))
	order, err = s.Execute(t.Context(), "bob", orderCommand("reject", order))
	require.NoError(t, err)
	cash = orderCommand("cash", order)
	cash.PaymentAdmin = "bob"
	order, err = s.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	result = runModernContinuation(t, f, 42203, identity.BobTelegramID, "Accept payment for "+order.ID, fmt.Sprintf(`
const stale=tools.orders.review.read({order_id:%q,resume:true});let accepted=false;try{tools.orders.review.decide({order_id:%q,name:"accept"});accepted=true;}catch(_){}return {stale,accepted};`, order.ID, order.ID))
	assert.JSONEq(t, `{"stale":{"error":"stale","restart":true},"accepted":false}`, string(result))
	current, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "cash", current.State)
}

func TestModernOrdersCompleteSinglePageResumeCannotAdoptChangedSnapshot(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	_ = runModernContinuation(
		t,
		f,
		42300,
		identity.AliceTelegramID,
		"Read "+order.ID,
		fmt.Sprintf(`return {more:tools.orders.inspect({order_id:%q}).more};`, order.ID),
	)
	changed := orderCommand("edit", order)
	changed.Choice = orderChoice("preparty")
	changed.Choice.Customer = "Changed"
	_, err = s.Execute(t.Context(), "alice", changed)
	require.NoError(t, err)
	result := runModernContinuation(
		t,
		f,
		42301,
		identity.AliceTelegramID,
		"Continue reading "+order.ID,
		fmt.Sprintf(`return tools.orders.inspect({order_id:%q,resume:true});`, order.ID),
	)
	assert.JSONEq(t, `{"error":"stale","restart":true}`, string(result))
}

func TestModernOrdersCompleteUncommittedReadCannotAuthorize(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`CREATE FUNCTION bot.reject_modern_read_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN IF NEW.kind='script_runs' AND NEW.content::text LIKE '%read_snapshot%' THEN RAISE EXCEPTION 'synthetic read receipt write failure'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER reject_modern_read_receipt BEFORE UPDATE ON bot.interactions FOR EACH ROW EXECUTE FUNCTION bot.reject_modern_read_receipt()`,
	)
	require.NoError(t, err)
	result := runModernContinuation(t, f, 42400, identity.AliceTelegramID, "Read then delete "+order.ID, fmt.Sprintf(`
let read=false,deleted=false;try{tools.orders.inspect({order_id:%q});read=true;}catch(_){}
try{tools.orders.update({name:"delete",order_id:%q});deleted=true;}catch(_){}return {read,deleted};`, order.ID, order.ID))
	assert.JSONEq(t, `{"read":false,"deleted":false}`, string(result))
	_, err = f.db.Exec(
		t.Context(),
		`DROP TRIGGER reject_modern_read_receipt ON bot.interactions; DROP FUNCTION bot.reject_modern_read_receipt()`,
	)
	require.NoError(t, err)
	result = runModernContinuation(
		t,
		f,
		42401,
		identity.AliceTelegramID,
		"Resume read then delete "+order.ID,
		fmt.Sprintf(`
let resumed=false,deleted=false;try{tools.orders.inspect({order_id:%q,resume:true});resumed=true;}catch(_){}
try{tools.orders.update({name:"delete",order_id:%q});deleted=true;}catch(_){}return {resumed,deleted};`, order.ID, order.ID),
	)
	assert.JSONEq(t, `{"resumed":false,"deleted":false}`, string(result))
	current, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order.Version, current.Version)
}
