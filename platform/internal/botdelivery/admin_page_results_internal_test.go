package botdelivery

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminPageResultsRejectUnrelatedEffects(t *testing.T) {
	t.Parallel()
	valid := AdminPageResultsRequest{Owner: "bob", Chat: 202, Update: 99, ID: 42,
		Effects: []AdminPageEffect{{Effect: "admin_page:42:0", Payload: adminPagePreparingPayload(42, "page")}}}
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
	tooLongPayload := adminPagePreparingPayload(42, strings.Repeat("🐈", 2049))
	tooLong.Effects = []AdminPageEffect{{Effect: "admin_page:42:0", Payload: tooLongPayload}}
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
		{Effect: final, Payload: adminPagePreparingPayload(message.ID, "private final chunk")},
	}}
	adminPageCaptureNative(t, db, settings.BotID, telegram.Update{ID: in.Update, Callback: &telegram.Callback{
		From: telegram.User{ID: in.Chat}, Data: fmt.Sprintf("adminmsg:results:%d", in.ID),
		Message: telegram.Message{Chat: telegram.Chat{ID: in.Chat, Type: "private"}},
	}})
	// A failure during the second enqueue must leave neither an authorized
	// partial snapshot nor any visible queue result from the first enqueue.
	_, err = db.Exec(ctx, `CREATE FUNCTION bot.synthetic_page_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (SELECT count(*) FROM bot.delivery_intents WHERE owner=NEW.owner)>0 THEN RAISE EXCEPTION 'synthetic enqueue failure'; END IF; RETURN NEW; END; $$;
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
	changed.Effects = []AdminPageEffect{
		{Effect: final, Payload: adminPagePreparingPayload(message.ID, "changed live page")},
	}
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
	var stale *core.ProblemError
	require.ErrorAs(t, s.EnqueueAdminPageResults(ctx, changed), &stale)
	require.Equal(t, "history_stale", stale.Code)
	require.Equal(t, []int64{2, 0, 2}, counts())
}

func adminPageCaptureNative(t *testing.T, db *pgxpool.Pool, botID int64, update telegram.Update) {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	var sender int64
	if update.Callback != nil {
		sender = update.Callback.From.ID
	} else {
		sender = update.Message.From.ID
	}
	require.NoError(t, registrationingress.SaveClassifiedNativeTelegram(t.Context(), tx,
		registrationingress.Reference{BotID: botID, UpdateID: update.ID}, sender, nil, update))
	require.NoError(t, tx.Commit(t.Context()))
}

func TestAdminPageResultsRequireOriginalNativeIntake(t *testing.T) {
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
	message, err := admin.Preview(ctx, "bob", "native-proof", adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "101"}}, Content: adminmessage.Content{Text: "private"},
	})
	require.NoError(t, err)
	s := Service{DB: db, Delivery: settings, AdminMessages: admin}
	in := AdminPageResultsRequest{Owner: "bob", Chat: 202, ID: message.ID, Effects: []AdminPageEffect{{
		Effect: fmt.Sprintf("admin_page:%d:0", message.ID), Payload: adminPagePreparingPayload(message.ID, "page"),
	}}}
	for index, test := range []struct {
		name        string
		bot, sender int64
		control     string
	}{
		{name: "absent"},
		{name: "foreign bot", bot: 78, sender: 202, control: fmt.Sprintf("adminmsg:results:%d", message.ID)},
		{name: "foreign owner and chat", bot: 77, sender: 101, control: fmt.Sprintf("adminmsg:results:%d", message.ID)},
		{name: "foreign page", bot: 77, sender: 202, control: fmt.Sprintf("adminmsg:results:%d", message.ID+1)},
		{name: "foreign offset", bot: 77, sender: 202, control: fmt.Sprintf("adminmsg:page:%d:20", message.ID)},
		{name: "foreign action", bot: 77, sender: 202, control: fmt.Sprintf("adminmsg:cancel:%d", message.ID)},
	} {
		in.Update = int64(100 + index)
		if test.bot != 0 {
			adminPageCaptureNative(t, db, test.bot, telegram.Update{ID: in.Update, Callback: &telegram.Callback{
				From: telegram.User{
					ID: test.sender,
				},
				Data:    test.control,
				Message: telegram.Message{Chat: telegram.Chat{ID: test.sender, Type: "private"}},
			}})
		}
		require.ErrorIs(t, s.EnqueueAdminPageResults(ctx, in), ErrBinding, test.name)
	}
	in.Update = 200
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	require.NoError(
		t,
		registrationingress.SaveTelegram(ctx, tx, registrationingress.Reference{BotID: 77, UpdateID: in.Update}, 202),
	)
	require.NoError(t, tx.Commit(ctx))
	require.ErrorIs(t, s.EnqueueAdminPageResults(ctx, in), ErrBinding, "legacy intake has no generation proof")
	in.Update = 201
	update := telegram.Update{ID: in.Update, Callback: &telegram.Callback{
		From: telegram.User{ID: 202}, Data: fmt.Sprintf("adminmsg:results:%d", message.ID),
		Message: telegram.Message{Chat: telegram.Chat{ID: 202, Type: "private"}},
	}}
	adminPageCaptureNative(t, db, 77, update)
	_, err = db.Exec(ctx, `INSERT INTO core.conversation_history_generations(owner,generation) VALUES('bob',1)`)
	require.NoError(t, err)
	// A native re-delivery after deletion cannot replace the original generation.
	adminPageCaptureNative(t, db, 77, update)
	var stale *core.ProblemError
	require.ErrorAs(t, s.EnqueueAdminPageResults(ctx, in), &stale)
	require.Equal(t, "history_stale", stale.Code)
	var generation, count int64
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT intake_generation FROM core.registration_ingress WHERE bot_id=77 AND request_key='201'`).
			Scan(&generation),
	)
	require.Zero(t, generation)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM bot.delivery_intents`).Scan(&count))
	require.Zero(t, count)
	_, err = db.Exec(
		ctx,
		`UPDATE core.registration_ingress SET intake_generation=1 WHERE bot_id=77 AND request_key='201'`,
	)
	require.Error(t, err, "durable generation is immutable")
}

func TestAdminPageResultsManualAndAgentNativeBindings(t *testing.T) {
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
	s := Service{DB: db, Delivery: settings, AdminMessages: admin}
	command := `/send_message_to 101 --msg "native manual"`
	preview, err := admin.PreviewCommand(ctx, "bob", "tg-admin-301", command)
	require.NoError(t, err)
	update := telegram.Update{
		ID: 301,
		Message: &telegram.Message{
			ID:   701,
			From: telegram.User{ID: 202},
			Chat: telegram.Chat{ID: 202, Type: "private"},
			Text: command,
		},
	}
	adminPageCaptureNative(t, db, 77, update)
	request := func(id, updateID int64) AdminPageResultsRequest {
		return AdminPageResultsRequest{
			Owner:  "bob",
			Chat:   202,
			Update: updateID,
			ID:     id,
			Effects: []AdminPageEffect{
				{Effect: fmt.Sprintf("admin_page:%d:0", id), Payload: adminPagePreparingPayload(id, "page")},
			},
		}
	}
	require.NoError(t, s.EnqueueAdminPageResults(ctx, request(preview.ID, 301)))
	// The same native update cannot select a different preview or offset.
	other, err := admin.Preview(
		ctx,
		"bob",
		"other",
		adminmessage.Request{
			Destinations: []adminmessage.Destination{{Chat: "101"}},
			Content:      adminmessage.Content{Text: "other"},
		},
	)
	require.NoError(t, err)
	require.ErrorIs(t, s.EnqueueAdminPageResults(ctx, request(other.ID, 301)), ErrBinding)
	update.ID = 302
	update.Message.Text = "show the saved campaign"
	adminPageCaptureNative(t, db, 77, update)
	records := fmt.Sprintf(
		`[{"history_generation":0,"calls":[{"source":{"generation":0,"authorities":[]},"outcome":{"name":"broadcasts.show","error":"interrupted"},"broadcast_review":{"owner":"bob","chat":202,"arguments":{"id":%d}}}]}]`,
		other.ID,
	)
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('bob',302,'script_runs',$1)`,
		json.RawMessage(records),
	)
	require.NoError(t, err)
	require.ErrorIs(t, s.EnqueueAdminPageResults(ctx, request(preview.ID, 302)), ErrBinding)
	require.NoError(t, s.EnqueueAdminPageResults(ctx, request(other.ID, 302)))
	// A terminal redaction cannot authorize a new manifest even with native intake.
	update.ID = 303
	adminPageCaptureNative(t, db, 77, update)
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('bob',303,'script_runs',$1)`,
		json.RawMessage(
			strings.Replace(records, `"history_generation":0`, `"history_generation":0,"history_redacted":true`, 1),
		),
	)
	require.NoError(t, err)
	require.ErrorIs(t, s.EnqueueAdminPageResults(ctx, request(other.ID, 303)), ErrBinding)
}

func TestAdminPageResultsAttachmentNativeBinding(t *testing.T) {
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
	input, err := admin.BeginInput(ctx, "bob", "attachment-input", "/send_message_to 101", 202)
	require.NoError(t, err)
	require.NoError(t, admin.RegisterPrompt(ctx, "bob", input.ID, 202, 701))
	update := telegram.Update{
		ID: 401,
		Message: &telegram.Message{
			ID:   702,
			From: telegram.User{ID: 202},
			Chat: telegram.Chat{ID: 202, Type: "private"},
			Text: "actual native attachment",
		},
	}
	adminPageCaptureNative(t, db, 77, update)
	key := "tg-admin-attach-401"
	require.NoError(
		t,
		admin.RegisterSource(
			ctx,
			adminmessage.Source{Actor: "bob", Key: key, ChatID: 202, MessageID: 702, HTML: update.Message.Text},
		),
	)
	preview, err := admin.AttachInput(
		ctx,
		"bob",
		adminmessage.Attachment{InputID: input.ID, ChatID: 202, PromptID: 701, Key: key},
	)
	require.NoError(t, err)
	s := Service{DB: db, Delivery: settings, AdminMessages: admin}
	in := AdminPageResultsRequest{Owner: "bob", Chat: 202, Update: 401, ID: preview.ID, Effects: []AdminPageEffect{{
		Effect: fmt.Sprintf("admin_page:%d:0", preview.ID), Payload: adminPagePreparingPayload(preview.ID, "page"),
	}}}
	require.NoError(t, s.EnqueueAdminPageResults(ctx, in))
	// Temporary source expiry cannot replace or invalidate the saved manifest.
	_, err = db.Exec(ctx, `DELETE FROM core.admin_message_sources WHERE actor='bob' AND key=$1`, key)
	require.NoError(t, err)
	require.NoError(t, s.EnqueueAdminPageResults(ctx, in))
}

func TestAdminPageNativeCaptureRestrictedRole(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	ctx := t.Context()
	var exists bool
	require.NoError(t, db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='zns_bot')`).Scan(&exists))
	if !exists {
		t.Skip("existing restricted zns_bot role required; no global role mutation")
	}
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SET LOCAL ROLE zns_bot`)
	require.NoError(t, err)
	update := telegram.Update{
		ID:       501,
		Callback: &telegram.Callback{From: telegram.User{ID: 202}, Data: "adminmsg:results:42"},
	}
	require.NoError(t, registrationingress.SaveClassifiedNativeTelegram(ctx, tx,
		registrationingress.Reference{BotID: 77, UpdateID: 501}, 202, nil, update))
	var owner string
	var generation int64
	require.NoError(
		t,
		tx.QueryRow(ctx, `SELECT intake_owner,intake_generation FROM core.registration_ingress WHERE bot_id=77 AND request_key='501'`).
			Scan(&owner, &generation),
	)
	require.Equal(t, "bob", owner)
	require.Zero(t, generation)
	var privateRead bool
	require.NoError(t, tx.QueryRow(ctx, `SELECT has_column_privilege(current_user,'core.users','username','SELECT')
 OR has_table_privilege(current_user,'core.conversation_events','SELECT')
 OR has_table_privilege(current_user,'core.conversation_summaries','SELECT')`).Scan(&privateRead))
	require.False(t, privateRead, "intake metadata grants must not grant private history or profile bodies")
}

func adminPagePreparingPayload(id int64, text string) telegram.Send {
	return telegram.Send{ChatID: 202, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{{
		{Text: "Results", Data: fmt.Sprintf("adminmsg:results:%d", id)},
		{Text: "Cancel", Data: fmt.Sprintf("adminmsg:cancel:%d", id)},
	}}}}
}

func TestAdminPageResultsRejectNoncanonicalKeyboards(t *testing.T) {
	t.Parallel()
	valid := AdminPageResultsRequest{Owner: "bob", Chat: 202, Update: 99, ID: 42,
		Effects: []AdminPageEffect{{Effect: "admin_page:42:0", Payload: adminPagePreparingPayload(42, "page")}}}
	for _, test := range []struct {
		name   string
		button telegram.Button
	}{
		{"long callback", telegram.Button{Text: "Results", Data: strings.Repeat("x", 65)}},
		{"foreign operation", telegram.Button{Text: "Results", Data: "pass:confirm"}},
		{"foreign page", telegram.Button{Text: "Results", Data: "adminmsg:results:43"}},
		{"URL", telegram.Button{Text: "Results", Data: "adminmsg:results:42", URL: "https://example.invalid"}},
		{"WebApp", telegram.Button{Text: "Results", Data: "adminmsg:results:42", WebApp: &telegram.WebApp{URL: "https://example.invalid"}}},
		{"empty label", telegram.Button{Data: "adminmsg:results:42"}},
		{"unbounded label", telegram.Button{Text: strings.Repeat("x", 257), Data: "adminmsg:results:42"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := valid
			payload := adminPagePreparingPayload(42, "page")
			payload.Markup.Rows[0][0] = test.button
			in.Effects = []AdminPageEffect{{Effect: "admin_page:42:0", Payload: payload}}
			require.ErrorIs(t, validateAdminPageResults(in), ErrBinding)
		})
	}
	ready := valid
	ready.Effects = []AdminPageEffect{
		{Effect: "admin_page:42:0", Payload: telegram.Send{ChatID: 202, Text: "page"}},
		{
			Effect: "admin_view:42:0",
			Payload: telegram.Send{ChatID: 202, Text: "view", Markup: telegram.Markup{Rows: [][]telegram.Button{
				{
					{Text: "Send", Data: "adminmsg:send:42"},
				},
				{{Text: "Results", Data: "adminmsg:results:42"}},
				{{Text: "Cancel", Data: "adminmsg:cancel:42"}},
			}}},
		},
	}
	require.NoError(t, validateAdminPageResults(ready))
	duplicate := ready
	duplicate.Effects = append([]AdminPageEffect(nil), ready.Effects...)
	duplicate.Effects = append(
		duplicate.Effects,
		AdminPageEffect{Effect: "admin_view:42:1", Payload: ready.Effects[1].Payload},
	)
	require.ErrorIs(t, validateAdminPageResults(duplicate), ErrBinding)
	tooManyRows := valid
	payload := valid.Effects[0].Payload
	payload.Markup.Rows = make([][]telegram.Button, 24)
	tooManyRows.Effects = []AdminPageEffect{{Effect: "admin_page:42:0", Payload: payload}}
	require.ErrorIs(t, validateAdminPageResults(tooManyRows), ErrBinding)
}
