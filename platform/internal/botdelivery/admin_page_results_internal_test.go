package botdelivery

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminPageResultsRejectUnrelatedEffects(t *testing.T) {
	t.Parallel()
	valid := AdminPageResultsRequest{Owner: "bob", Chat: 202, Update: 99, ID: 42,
		Effects: []AdminPageEffect{{Effect: "admin_page:42:0", Payload: telegram.Send{ChatID: 202, Text: "page"}}}}
	require.NoError(t, validateAdminPageResults(valid))
	for _, effect := range []string{"", "unrelated", "admin_page:43:0", "admin_page:42:0:chunk:1", "admin_view:42:0"} {
		t.Run(effect, func(t *testing.T) {
			t.Parallel()
			in := valid
			in.Effects = []AdminPageEffect{{Effect: effect, Payload: valid.Effects[0].Payload}}
			require.ErrorIs(t, validateAdminPageResults(in), ErrBinding)
		})
	}
	tooMany := valid
	tooMany.Effects = nil
	for index := range 64 {
		tooMany.Effects = append(tooMany.Effects, AdminPageEffect{
			Effect: fmt.Sprintf("admin_page:42:0:chunk:%d", index), Payload: valid.Effects[0].Payload,
		})
	}
	tooMany.Effects = append(tooMany.Effects, valid.Effects...)
	require.ErrorIs(t, validateAdminPageResults(tooMany), ErrBinding)
	wrongChat := valid
	wrongChat.Chat++
	require.ErrorIs(t, validateAdminPageResults(wrongChat), ErrBinding)
	tooLong := valid
	tooLong.Effects = []AdminPageEffect{
		{Effect: "admin_page:42:0", Payload: telegram.Send{ChatID: 202, Text: strings.Repeat("🐈", 2049)}},
	}
	require.Error(t, validateAdminPageResults(tooLong))
}

func TestAdminPageResultsAtomicReplayAuthorityAndErasure(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `INSERT INTO core.pass_booking_admins(owner) VALUES('bob') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	settings := delivery.Settings{
		BotID:        77,
		BotInterval:  time.Microsecond,
		ChatInterval: time.Microsecond,
		Fallback:     time.Second,
	}
	admin := adminmessage.Service{DB: db, Delivery: settings}
	message, err := admin.Preview(ctx, "bob", "snapshot", adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "101"}}, Content: adminmessage.Content{Text: "private source"},
	})
	require.NoError(t, err)
	s := Service{DB: db, Delivery: settings, AdminMessages: admin}
	final := fmt.Sprintf("admin_page:%d:0", message.ID)
	in := AdminPageResultsRequest{Owner: "bob", Chat: 202, Update: 99, ID: message.ID, Effects: []AdminPageEffect{
		{Effect: final + ":chunk:0", Payload: telegram.Send{ChatID: 202, Text: "private first chunk"}},
		{Effect: final, Payload: telegram.Send{ChatID: 202, Text: "private final chunk"}},
	}}
	// A failure during the second enqueue must leave neither an authorized
	// partial snapshot nor any visible queue result from the first enqueue.
	_, err = db.Exec(ctx, `CREATE FUNCTION bot.synthetic_page_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (SELECT count(*) FROM bot.delivery_intents WHERE owner=NEW.owner)>0 THEN RAISE EXCEPTION 'synthetic enqueue failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER synthetic_page_fail BEFORE INSERT ON bot.delivery_intents FOR EACH ROW EXECUTE FUNCTION bot.synthetic_page_fail()`)
	require.NoError(t, err)
	require.Error(t, s.EnqueueAdminPageResults(ctx, in))
	counts := func() []int64 {
		var intents, bodies, queue int64
		require.NoError(t, db.QueryRow(ctx, `SELECT (SELECT count(*) FROM bot.delivery_intents),
 (SELECT count(*) FROM bot.interactions WHERE kind LIKE 'delivery_result:%'),(SELECT count(*) FROM core.delivery_queue)`).Scan(&intents, &bodies, &queue))
		return []int64{intents, bodies, queue}
	}
	require.Equal(t, []int64{0, 0, 0}, counts())
	_, err = db.Exec(
		ctx,
		`DROP TRIGGER synthetic_page_fail ON bot.delivery_intents; DROP FUNCTION bot.synthetic_page_fail()`,
	)
	require.NoError(t, err)
	require.NoError(t, s.EnqueueAdminPageResults(ctx, in))
	require.Equal(t, []int64{2, 3, 2}, counts())
	// Even a simulated lost child is resumed from the saved complete manifest.
	operation, effect := ResultOperation(in.Owner, in.Update, final)
	_, err = db.Exec(
		ctx,
		`DELETE FROM core.delivery_queue WHERE bot_id=77 AND owner_key=$1 AND effect_key=$2`,
		operation,
		effect,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		ctx,
		`DELETE FROM bot.delivery_intents WHERE bot_id=77 AND operation_key=$1 AND effect_key=$2`,
		operation,
		effect,
	)
	require.NoError(t, err)
	changed := in
	changed.Effects = []AdminPageEffect{{Effect: final, Payload: telegram.Send{ChatID: 202, Text: "changed live page"}}}
	require.NoError(t, s.EnqueueAdminPageResults(ctx, changed))
	require.Equal(t, []int64{2, 3, 2}, counts())
	var body string
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT content->'payload'->>'text' FROM bot.interactions WHERE owner='bob' AND update_id=99 AND kind=$1`, "delivery_result:"+effect).
			Scan(&body),
	)
	require.Equal(t, "private final chunk", body)
	_, err = db.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	require.Error(t, s.EnqueueAdminPageResults(ctx, changed))
	require.Equal(t, []int64{2, 3, 2}, counts())
	_, err = db.Exec(ctx, `INSERT INTO core.pass_booking_admins(owner) VALUES('bob');
 INSERT INTO core.conversation_history_generations(owner,generation) VALUES('bob',1) ON CONFLICT(owner) DO UPDATE SET generation=core.conversation_history_generations.generation+1`)
	require.NoError(t, err)
	require.Equal(t, []int64{2, 0, 2}, counts())
	require.ErrorIs(t, s.EnqueueAdminPageResults(ctx, changed), ErrStale)
	require.Equal(t, []int64{2, 0, 2}, counts())
}
