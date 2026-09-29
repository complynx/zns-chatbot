package integration_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const legacyOrderObjectID = "0123456789abcdef01234567"

func legacyCallbackFixture(t *testing.T) (orders.Service, orders.Order) {
	t.Helper()
	service, order := legacyCashFixture(t, false)
	service.LegacyBotID = 77
	_, err := service.DB.Exec(t.Context(), `UPDATE core.legacy_order_import_references
	SET source_record=jsonb_set(source_record,'{_id}', $1::jsonb) WHERE target_id=$2`,
		`{"$oid":"`+legacyOrderObjectID+`"}`, order.ID)
	require.NoError(t, err)
	return service, order
}

func TestLegacyOrderCallbackResolutionAndCurrentAccess(t *testing.T) {
	t.Parallel()
	service, order := legacyCallbackFixture(t)
	request := orders.LegacyCallbackRequest{
		Data:  "orders|adm_acc|" + legacyOrderObjectID,
		Event: "not-the-source-event",
	}
	binding, err := service.ResolveLegacyCallback(t.Context(), "bob", request)
	require.NoError(t, err)
	assert.Equal(t, order.EventID, binding.EventID)
	assert.Equal(t, order.ID, binding.OrderID)
	assert.Equal(t, order.Version, binding.Command.Version)
	assert.Empty(t, binding.Command.Attempt)
	assert.Equal(t, "accept", binding.Command.Name)
	_, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	requireCode(t, err, "forbidden")
	request.Data = "orders|del|" + legacyOrderObjectID
	_, err = service.ResolveLegacyCallback(t.Context(), "bob", request)
	requireCode(t, err, "order_not_found")
	_, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	require.NoError(t, err)
	service.LegacyBotID = 88
	_, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	requireCode(t, err, "order_not_found")
	service.LegacyBotID = 0
	_, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	requireCode(t, err, "legacy_callbacks_disabled")
	service.LegacyBotID = 77
	_, err = service.DB.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	_, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	requireCode(t, err, "forbidden")
	request.Data, request.Event = "orders|xlsx", order.EventID
	_, err = service.ResolveLegacyCallback(t.Context(), "bob", request)
	require.NoError(t, err)
	_, err = service.DB.Exec(t.Context(), `DELETE FROM core.order_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.ResolveLegacyCallback(t.Context(), "bob", request)
	requireCode(t, err, "forbidden")
}

func TestLegacyOrderCallbackRefAmbiguityAndAttemptGuard(t *testing.T) {
	t.Parallel()
	service, order := legacyCallbackFixture(t)
	request := orders.LegacyCallbackRequest{Data: "orders|pcancel|" + legacyOrderObjectID}
	_, err := service.ResolveLegacyCallback(t.Context(), "alice", request)
	requireCode(t, err, "payment_locked")
	request.Data = "orders|adm_rej|" + legacyOrderObjectID
	binding, err := service.ResolveLegacyCallback(t.Context(), "bob", request)
	require.NoError(t, err)
	binding.Command.Key = "legacy-cancel"
	finished, err := service.Execute(t.Context(), "bob", binding.Command)
	require.NoError(t, err)
	replayed, err := service.Execute(t.Context(), "bob", binding.Command)
	require.NoError(t, err)
	assert.True(t, finished.CreatedAt.Equal(replayed.CreatedAt))
	replayed.CreatedAt = finished.CreatedAt
	assert.Equal(t, finished, replayed)
	cash := orderCommand("cash", finished)
	cash.PaymentAdmin = "bob"
	_, err = service.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	_, err = service.ResolveLegacyCallback(t.Context(), "bob", request)
	requireCode(t, err, "stale_attempt")
	raw, err := json.Marshal(map[string]any{"_id": map[string]string{"$oid": legacyOrderObjectID}})
	require.NoError(t, err)
	_, err = service.DB.Exec(t.Context(), `INSERT INTO core.legacy_order_import_references
	(source_key,bot_id,event_id,source_domain,source_record_sha256,target_id,source_record)
	VALUES($1,77,$2,'orders',$3,'another-target',$4)`, strings.Repeat("c", 64), order.EventID, strings.Repeat("d", 64), raw)
	require.NoError(t, err)
	request.Data = "orders|pay|" + legacyOrderObjectID
	_, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	requireCode(t, err, "order_not_found")
}

func TestLegacyOrderCallbackProofAndPaymentRouting(t *testing.T) {
	t.Parallel()
	f, service, order := legacyBotFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES('bob','synthetic','bob');
	INSERT INTO core.telegram_identities(bot_id,telegram_id,owner) VALUES(77,202,'bob');
	INSERT INTO core.order_admins(event_id,owner,country,region) VALUES('sandbox-festival','bob','ru','Synthetic') ON CONFLICT DO NOTHING`,
	)
	require.NoError(t, err)
	request := orders.LegacyCallbackRequest{Data: "orders|cash|" + legacyOrderObjectID + "|202"}
	binding, err := service.ResolveLegacyCallback(t.Context(), "alice", request)
	require.NoError(t, err)
	assert.Equal(t, "bob", binding.Command.PaymentAdmin)
	assert.Equal(t, "cash", binding.Command.Name)
	proof := orderCommand("proof", order)
	proof.ProofFile = uploadProof(t, service, "alice")
	order, err = service.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	order.Attempt = "source-token"
	_, err = f.db.Exec(t.Context(), `UPDATE core.orders SET attempt=$2 WHERE id=$1`, order.ID, order.Attempt)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.legacy_order_import_references SET source_record=source_record || jsonb_build_object('payment_attempt_token',$1::text,'proof_file','source-file') WHERE target_id=$2`,
		order.Attempt,
		order.ID,
	)
	require.NoError(t, err)
	request.Data = "orders|adm_acc|" + legacyOrderObjectID + "|" + order.Attempt
	binding, err = service.ResolveLegacyCallback(t.Context(), "bob", request)
	require.NoError(t, err)
	assert.Equal(t, order.Attempt, binding.Command.Attempt)
	request.Data = "orders|adm_acc|" + legacyOrderObjectID
	_, err = service.ResolveLegacyCallback(t.Context(), "bob", request)
	requireCode(t, err, "stale_attempt")
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.legacy_order_import_references
	(source_key,bot_id,event_id,source_domain,source_record_sha256,target_id,source_record)
	VALUES($1,77,$2,'configuration',$3,$2,'{"payment_admin_ru":202}')`, strings.Repeat("c", 64), order.EventID, strings.Repeat("d", 64))
	require.NoError(t, err)
	request.Data = "orders|paid|" + legacyOrderObjectID
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.order_admins SET country='ru' WHERE owner='bob' AND event_id=$1`,
		order.EventID,
	)
	require.NoError(t, err)
	binding, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	require.NoError(t, err)
	assert.Equal(t, "ru", binding.Command.Country)
	assert.Equal(t, "bob", binding.Command.PaymentAdmin)
	_, err = f.db.Exec(t.Context(), `UPDATE core.orders SET attempt='legacy-proof:source-file' WHERE id=$1`, order.ID)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.legacy_order_import_references SET source_record=source_record-'payment_attempt_token' WHERE target_id=$1`,
		order.ID,
	)
	require.NoError(t, err)
	request.Data = "orders|pcancel|" + legacyOrderObjectID
	binding, err = service.ResolveLegacyCallback(t.Context(), "alice", request)
	require.NoError(t, err)
	binding.Command.Key = "legacy-proof-cancel"
	finished, err := service.Execute(t.Context(), "alice", binding.Command)
	require.NoError(t, err)
	assert.Equal(t, "unpaid", finished.State)
}
