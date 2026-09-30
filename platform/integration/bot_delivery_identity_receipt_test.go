package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type receiptCompletionTransport struct {
	cancel context.CancelFunc
}

func (t receiptCompletionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	t.cancel()
	return response, nil
}

func receiptIdentityFixture(t *testing.T) (*fixture, delivery.Reference) {
	t.Helper()
	f := memorySplitRoleFixture(t)
	f.b.Delivery.Fallback = 50 * time.Millisecond
	links := identity.Links{DB: f.db, Issuer: "https://identity.invalid", BotID: 77}
	require.NoError(t, links.Bind(t.Context(), "alice", identity.AliceTelegramID, "alice-provider"))
	f.b.API.Links = links
	f.b.API.Exchange = provisioningExchange{links: links, signer: f.b.Host.Signer}
	live, owner, err := f.b.API.AuthenticateTelegram(t.Context(), identity.AliceTelegramID)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderProfile(live, owner, identity.AliceTelegramID))
	ref := delivery.Reference{Owner: delivery.Bot}
	require.NoError(t, f.b.DB.QueryRow(t.Context(),
		`SELECT operation_key,effect_key FROM bot.delivery_intents WHERE owner='alice' AND reference->>'kind'='card'`).
		Scan(&ref.Key, &ref.Effect))
	return f, ref
}

func receiptIdentityIntent(t *testing.T, f *fixture, ref delivery.Reference) botdelivery.Intent {
	t.Helper()
	intent, err := botdelivery.Read(t.Context(), f.b.DB, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	return intent
}

func TestBotDeliveryReceiptAuthenticatedCompletion(t *testing.T) {
	t.Parallel()
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("shutdown_%t", shutdown), func(t *testing.T) {
			t.Parallel()
			f, ref := receiptIdentityFixture(t)
			worker, cancel := context.WithCancel(t.Context())
			defer cancel()
			if shutdown {
				f.b.TG.HTTP = &http.Client{Transport: receiptCompletionTransport{cancel: cancel}}
			}
			require.NoError(t, f.b.DeliverBotIntent(worker, ref))
			if shutdown {
				require.ErrorIs(t, worker.Err(), context.Canceled)
			}
			intent := receiptIdentityIntent(t, f, ref)
			require.Equal(t, delivery.Succeeded, intent.State)
			require.True(t, intent.ContinuationDone)
			var messageID int64
			require.NoError(t, f.db.QueryRow(t.Context(),
				`SELECT message_id FROM bot.order_cards WHERE owner='alice' AND card_key='profile'`).Scan(&messageID))
			require.Equal(t, intent.MessageID, messageID)
		})
	}
}

func TestBotDeliveryReceiptAuthenticatedRecovery(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch_%t", batch), func(t *testing.T) {
			t.Parallel()
			f, ref := receiptIdentityFixture(t)
			wire := &boundaryTransport{}
			f.b.TG.HTTP = &http.Client{Transport: wire}
			f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
				if strings.HasSuffix(r.URL.Path, "/receipt") {
					return errors.New("synthetic receipt outage")
				}
				return nil
			}}}
			require.Error(t, f.b.DeliverBotIntent(t.Context(), ref))
			intent := receiptIdentityIntent(t, f, ref)
			require.Equal(t, delivery.Succeeded, intent.State)
			require.False(t, intent.ContinuationDone)
			require.EqualValues(t, 1, wire.calls.Load())
			restarted := *f.b
			restarted.Host.HTTP = nil
			recoverReceipt := func() error {
				if batch {
					return restarted.ContinueBotIntentReceipts(t.Context())
				}
				return restarted.DeliverBotIntent(t.Context(), ref)
			}
			_, err := f.db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=false WHERE owner='alice'`)
			require.NoError(t, err)
			waitReceiptDeadlines(t, f)
			require.ErrorIs(t, recoverReceipt(), identity.ErrZitadelIdentity)
			require.False(t, receiptIdentityIntent(t, f, ref).ContinuationDone)
			_, err = f.db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=true WHERE owner='alice'`)
			require.NoError(t, err)
			waitReceiptDeadlines(t, f)
			require.NoError(t, recoverReceipt())
			require.True(t, receiptIdentityIntent(t, f, ref).ContinuationDone)
			require.NoError(t, recoverReceipt())
			require.EqualValues(t, 1, wire.calls.Load(), "receipt recovery must not resend transport")
		})
	}
}

func waitReceiptDeadlines(t *testing.T, f *fixture) {
	t.Helper()
	require.Eventually(t, func() bool {
		var ready bool
		err := f.db.QueryRow(t.Context(), `SELECT NOT EXISTS(SELECT 1 FROM bot.delivery_intents
 WHERE state='sent' AND NOT continuation_done AND not_before>clock_timestamp())`).Scan(&ready)
		return err == nil && ready
	}, 5*time.Second, 5*time.Millisecond)
}

func TestBotDeliveryReceiptDeniedBatchCannotStarveHealthyOwner(t *testing.T) {
	t.Parallel()
	f, first := receiptIdentityFixture(t)
	links, ok := f.b.API.Links.(identity.Links)
	require.True(t, ok)
	require.NoError(t, links.Bind(t.Context(), "bob", identity.BobTelegramID, "bob-provider"))
	wire := &boundaryTransport{}
	f.b.TG.HTTP = &http.Client{Transport: wire}
	f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
		if strings.HasSuffix(r.URL.Path, "/receipt") {
			return errors.New("synthetic receipt outage")
		}
		return nil
	}}}
	require.Error(t, f.b.DeliverBotIntent(t.Context(), first))
	require.Equal(t, delivery.Succeeded, receiptIdentityIntent(t, f, first).State)
	for n := range 31 {
		queueSentReceipt(t, f, "alice", identity.AliceTelegramID, int64(45000+n))
	}
	healthy := queueSentReceipt(t, f, "bob", identity.BobTelegramID, 46000)
	require.EqualValues(t, 33, wire.calls.Load())
	f.b.Host.HTTP = nil
	waitReceiptDeadlines(t, f)
	_, err := f.db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=false WHERE owner='alice'`)
	require.NoError(t, err)
	require.ErrorIs(t, f.b.ContinueBotIntentReceipts(t.Context()), identity.ErrZitadelIdentity)
	require.False(
		t,
		receiptIdentityIntent(t, f, healthy).ContinuationDone,
		"first batch contains exactly 32 denied receipts",
	)
	_ = f.b.ContinueBotIntentReceipts(t.Context())
	require.True(
		t,
		receiptIdentityIntent(t, f, healthy).ContinuationDone,
		"healthy tail must advance past denied receipts",
	)
	var deniedPending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE owner='alice' AND state='sent' AND NOT continuation_done`).Scan(&deniedPending))
	require.Equal(t, 32, deniedPending, "denied receipts must remain incomplete and recoverable")
	_, err = f.db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=true WHERE owner='alice'`)
	require.NoError(t, err)
	waitReceiptDeadlines(t, f)
	require.NoError(t, f.b.ContinueBotIntentReceipts(t.Context()))
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE state='sent' AND NOT continuation_done`).Scan(&pending))
	require.Zero(t, pending)
	require.EqualValues(t, 33, wire.calls.Load(), "receipt retries must never resend successful transport")
}

func queueSentReceipt(t *testing.T, f *fixture, owner string, chat, update int64) delivery.Reference {
	t.Helper()
	live, resolved, err := f.b.API.AuthenticateTelegram(t.Context(), chat)
	require.NoError(t, err)
	require.Equal(t, owner, resolved)
	in := boundaryResult("receipt_fairness")
	in.Owner, in.Chat, in.Update = owner, chat, update
	require.NoError(t, f.b.Host.EnqueueBotResult(live, in))
	i := boundaryIntent(t, f, in)
	require.Error(t, f.b.DeliverBotIntent(t.Context(), i.QueueReference()))
	require.Equal(t, delivery.Succeeded, receiptIdentityIntent(t, f, i.QueueReference()).State)
	return i.QueueReference()
}
