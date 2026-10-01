package bot

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func botIntentTestSettings() delivery.Settings {
	return delivery.Settings{
		BotID:        77,
		BotInterval:  time.Microsecond,
		ChatInterval: time.Microsecond,
		Fallback:     time.Second,
	}
}

func TestBotDeliveryPostgresRecoveryRetainsOrderAndAttemptFence(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	b := botDeliveryTestBot(db)
	ctx := t.Context()
	ref := botdelivery.Reference{
		Kind:     botdelivery.IdentityIntent,
		Update:   1,
		Notice:   i18n.IdentityUnavailable,
		Language: "en",
	}
	first, err := b.enqueueBotIntent(ctx, "", 101, "identity:1", "unavailable", ref, "send")
	require.NoError(t, err)
	ref.Update = 2
	second, err := b.enqueueBotIntent(ctx, "", 101, "identity:2", "unavailable", ref, "send")
	require.NoError(t, err)
	firstIntent, err := botdelivery.Read(ctx, db, 77, first.Reference, false)
	require.NoError(t, err)
	attempt, ready, err := b.beginBotIntent(ctx, firstIntent, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	secondIntent, err := botdelivery.Read(ctx, db, 77, second.Reference, false)
	require.NoError(t, err)
	_, ready, err = b.beginBotIntent(ctx, secondIntent, botRenderedDelivery{})
	require.NoError(t, err)
	require.False(t, ready)
	require.NoError(t, b.RecoverBotIntents(ctx))
	recovered, err := botdelivery.Read(ctx, db, 77, first.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, recovered.State)
	require.NoError(t, b.RecoverBotIntents(ctx))
	unchanged, err := botdelivery.Read(ctx, db, 77, first.Reference, false)
	require.NoError(t, err)
	require.Equal(t, recovered.NotBefore, unchanged.NotBefore, "pending recovery must not postpone its deadline")
	_, ready, err = b.beginBotIntent(ctx, secondIntent, botRenderedDelivery{})
	require.NoError(t, err)
	require.False(t, ready)
	time.Sleep(time.Until(recovered.NotBefore) + 20*time.Millisecond)
	retry, ready, err := b.beginBotIntent(ctx, recovered, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, attempt.Attempt+1, retry.Attempt)
	require.ErrorIs(t, b.finishBotIntent(
		ctx,
		attempt,
		delivery.Outcome{
			Kind:      delivery.Succeeded,
			MessageID: 899,
		},
		botdelivery.Continuation{},
		false,
	), botdelivery.ErrBinding)
	require.NoError(
		t,
		b.finishBotIntent(
			ctx,
			retry,
			delivery.Outcome{Kind: delivery.Succeeded, MessageID: 900},
			botdelivery.Continuation{},
			false,
		),
	)
	finished, err := botdelivery.Read(ctx, db, 77, first.Reference, false)
	require.NoError(t, err)
	require.Equal(t, int64(900), finished.MessageID)
	_, ready, err = b.beginBotIntent(ctx, secondIntent, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	stale := retry
	stale.Attempt++
	require.Error(
		t,
		b.finishBotIntent(
			ctx,
			stale,
			delivery.Outcome{Kind: delivery.Succeeded, MessageID: 901},
			botdelivery.Continuation{},
			false,
		),
	)
}

func TestBotDeliveryPostgresPrivateBodyDeletedWithoutEffectRevival(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, "UPDATE core.users SET telegram_id=813123 WHERE id='alice'")
	require.NoError(t, err)
	b := botDeliveryTestBot(db)
	result := botdelivery.StoredResult{Payload: telegram.Send{Text: "private delivery canary"}}
	require.NoError(
		t,
		b.queueBotResult(ctx, "alice", 813123, 7, "answer", botdelivery.Reference{Family: "static"}, result, 0),
	)
	operation, effect := botdelivery.ResultOperation("alice", 7, "answer")
	key := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	intent, err := botdelivery.Read(ctx, db, 77, key, false)
	require.NoError(t, err)
	_, err = db.Exec(
		ctx,
		"INSERT INTO core.conversation_history_generations(owner,generation) VALUES('alice',1) ON CONFLICT(owner) DO UPDATE SET generation=1",
	)
	require.NoError(t, err)
	var bodies int
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT count(*) FROM bot.interactions WHERE owner='alice' AND kind LIKE 'delivery_result:%'").
			Scan(&bodies),
	)
	require.Zero(t, bodies)
	_, ready, err := b.beginBotIntent(ctx, intent, botRenderedDelivery{})
	require.Error(t, err)
	require.False(t, ready)
	require.NoError(
		t,
		b.queueBotResult(ctx, "alice", 813123, 7, "answer", botdelivery.Reference{Family: "static"}, result, 0),
	)
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT count(*) FROM bot.interactions WHERE owner='alice' AND kind LIKE 'delivery_result:%'").
			Scan(&bodies),
	)
	require.Zero(t, bodies, "a replay must not create a private body in the new generation")
	require.NoError(t, b.postponeBotIntent(context.Background(), intent, true))
	cancelled, err := botdelivery.Read(ctx, db, 77, key, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Cancelled, cancelled.State)
}

func TestBotDeliveryPostgresCardReplayPreservesBinding(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := withBotDeliveryOrigin(t.Context(), delivery.Reference{Owner: delivery.Massage, Key: "42", Effect: "refresh"})
	_, err := db.Exec(ctx, "UPDATE core.users SET telegram_id=813123 WHERE id='alice'")
	require.NoError(t, err)
	b := botDeliveryTestBot(db)
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.pass_views(owner,chat_id,revision,state) VALUES('alice',813123,1,'{"view":"events"}')`,
	)
	require.NoError(t, err)
	ref := botdelivery.Reference{Family: botFamilyPasses, CardKey: botFamilyPasses, Revision: 1}
	payload := telegram.Send{ChatID: 813123, Text: "original card"}
	require.NoError(t, b.queueBotCard(ctx, "alice", payload, ref, botdelivery.Continuation{Kind: botPassCardReceipt}))
	operation, effect, child, err := botDeliveryChild(ctx, ref)
	require.NoError(t, err)
	require.True(t, child)
	key := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	original, err := botdelivery.Read(ctx, db, 77, key, false)
	require.NoError(t, err)
	ref.Revision = 2
	require.NoError(t, b.queueBotCard(ctx, "alice", payload, ref, botdelivery.Continuation{Kind: botPassCardReceipt}))
	replay, err := botdelivery.Read(ctx, db, 77, key, false)
	require.NoError(t, err)
	require.Equal(t, original.Reference, replay.Reference)
	require.Equal(t, delivery.Deferred, replay.State)
	payload.ChatID++
	require.ErrorIs(
		t,
		b.queueBotCard(ctx, "alice", payload, ref, botdelivery.Continuation{Kind: botPassCardReceipt}),
		botdelivery.ErrBinding,
	)
	var count int
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT count(*) FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2", 77, operation).
			Scan(&count),
	)
	require.Equal(t, 1, count)
}

func botDeliveryTestBot(db *pgxpool.Pool) Bot {
	settings := botIntentTestSettings()
	authorizer := applicationauth.Authorizer{
		DB:     db,
		Verify: func(_ context.Context, token string) (string, error) { return token, nil },
	}
	return Bot{
		DB:       db,
		Delivery: settings,
		API: appclient.Client{
			SandboxToken: func(owner string) string { return owner },
			LocalHistory: &appclient.LocalHistory{Service: conversation.Service{DB: db}, Authorizer: authorizer},
		},
		Host: appclient.Host{
			LocalBotDelivery: &appclient.LocalBotDelivery{
				Service:    botdelivery.Service{DB: db, Delivery: settings},
				Authorizer: authorizer,
			},
			UserToken: func(_ context.Context, owner string) (string, error) { return owner, nil },
		},
	}
}
