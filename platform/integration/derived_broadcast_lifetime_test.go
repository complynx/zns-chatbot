package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	deliverypolicy "github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDerivedBroadcastRetainsSourceUntilPublication(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"preview", "queued"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx := t.Context()
			_, err := f.db.Exec(ctx, `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
			require.NoError(t, err)
			history := conversation.Service{DB: f.db}
			require.NoError(
				t,
				history.AppendOriginal(ctx, "alice", "broadcast-source", "user", "private broadcast canary"),
			)
			var original int64
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='broadcast-source'`).
					Scan(&original),
			)
			f.b.Scripts = scopeVM{}
			raw := broadcastScript(
				t,
				f,
				29100,
				`return await tools.broadcasts.preview({command:input.command});`,
				map[string]string{"command": `/send_message_to 202 --msg "private broadcast canary"`},
			)
			var preview struct {
				ID                   int64 `json:"id"`
				ConfirmationRequired bool  `json:"confirmation_required"`
			}
			require.NoError(t, json.Unmarshal(raw, &preview))
			require.Positive(t, preview.ID)
			require.True(t, preview.ConfirmationRequired)
			require.NoError(t, f.b.DeliverAdminMessages(ctx))
			require.Empty(t, chatMessages(t, f, 202), "an agent preview is not publication approval")
			service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: f.db}
			if stage == "queued" {
				callback := message(29101, 101, "")
				callback.Message = nil
				callback.Callback = &telegram.Callback{
					ID:      "29101",
					From:    telegram.User{ID: 101},
					Message: telegram.Message{Chat: telegram.Chat{ID: 101, Type: "private"}},
					Data:    fmt.Sprintf("adminmsg:send:%d", preview.ID),
				}
				handle(t, f.b, callback)
				var queued int
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.admin_message_deliveries WHERE message_id=$1`, preview.ID).
						Scan(&queued),
				)
				require.Equal(t, 1, queued)
			}
			require.NoError(t, history.DeleteContent(ctx, "alice", original))
			if stage == "preview" {
				page, readErr := service.Review(ctx, "alice", preview.ID, 0)
				require.Error(t, readErr, "saved derived preview must not outlive its source")
				require.Empty(t, page.Items)
				require.Error(t, service.Enqueue(ctx, "alice", preview.ID))
			} else {
				require.NoError(t, f.b.DeliverAdminMessages(ctx))
				require.Empty(t, chatMessages(t, f, 202), "manual approval does not erase causal revocation")
			}
		})
	}
}

func TestDerivedBroadcastManualPublicationBindsFrozenContent(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	_, err := f.db.Exec(ctx, `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	generation := int64(0)
	source := readsource.Derivation{
		Generation:     &generation,
		Authorities:    []readsource.Authority{},
		PrivateHistory: true,
	}
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: f.db}
	draft, err := service.PreviewDerivedCommand(
		ctx,
		"alice",
		"publication",
		`/send_message_to -100123 --msg "approved frozen content"`,
		source,
	)
	require.NoError(t, err)
	_, found, err := service.Claim(ctx)
	require.NoError(t, err)
	require.False(t, found)
	_, err = service.PreviewCommand(ctx, "alice", "publication", `/send_message_to 202 --msg "replacement"`)
	requireCode(t, err, "idempotency_conflict")
	require.NoError(t, service.Enqueue(ctx, "alice", draft.ID))
	delivery, found, err := service.Claim(ctx)
	require.NoError(t, err)
	require.True(t, found, "manual publication supports unmapped group recipients")
	require.Equal(t, "-100123", delivery.Destination.Chat)
	require.Equal(t, "approved frozen content", delivery.Content.Text)
	gate, err := service.BeginDelivery(ctx, deliverypolicy.Attempt{ID: delivery.ID, Generation: delivery.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(t, service.Complete(ctx, delivery.ID, delivery.Attempt, 901, "", false))
	require.Error(
		t,
		service.Complete(ctx, delivery.ID, delivery.Attempt, 901, "", false),
		"attempt fence remains in force",
	)
}

func TestDerivedBroadcastManualAttachmentRetainsPendingLineage(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	_, err := f.db.Exec(ctx, `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	history := conversation.Service{DB: f.db}
	require.NoError(t, history.AppendOriginal(ctx, "alice", "pending-source", "user", "private recipient selection"))
	var original int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='pending-source'`).
			Scan(&original),
	)
	generation := int64(0)
	source := readsource.Derivation{
		Generation:     &generation,
		Authorities:    []readsource.Authority{},
		PrivateHistory: true,
	}
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: f.db}
	input, err := service.BeginDerivedInput(ctx, "alice", "derived-pending", "/send_message_to 202", 101, source)
	require.NoError(t, err)
	require.NoError(t, service.RegisterPrompt(ctx, "alice", input.ID, 101, 50))
	require.NoError(
		t,
		service.RegisterSource(
			ctx,
			adminmessage.Source{
				Actor:     "alice",
				Key:       "manual-attachment",
				ChatID:    101,
				MessageID: 51,
				HTML:      "manual attached body",
			},
		),
	)
	draft, err := service.AttachInput(
		ctx,
		"alice",
		adminmessage.Attachment{InputID: input.ID, ChatID: 101, PromptID: 50, Key: "manual-attachment"},
	)
	require.NoError(t, err)
	require.NoError(t, history.DeleteContent(ctx, "alice", original))
	requireCode(t, service.Enqueue(ctx, "alice", draft.ID), "source_revoked")
	page, err := service.Review(ctx, "alice", draft.ID, 0)
	requireCode(t, err, "source_revoked")
	require.Empty(t, page.Items)
}

func TestDerivedBroadcastProviderReturnCannotPersistRevokedContent(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	ctx := t.Context()
	history := conversation.Service{DB: db}
	require.NoError(t, history.AppendOriginal(ctx, "bob", "render-source", "user", "private rendering context"))
	var original int64
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='bob' AND source_key='render-source'`).
			Scan(&original),
	)
	_, err := db.Exec(
		ctx,
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"Alice"}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	calls := 0
	service := adminmessage.Service{
		Delivery: syntheticDeliverySettings(),
		DB:       db,
		InformalName: func(callCtx context.Context, _ map[string]any) (string, error) {
			calls++
			return "revoked generated canary", history.DeleteContent(callCtx, "bob", original)
		},
	}
	generation := int64(0)
	source := readsource.Derivation{
		Generation:     &generation,
		Authorities:    []readsource.Authority{},
		PrivateHistory: true,
	}
	_, err = service.PreviewDerivedCommand(
		ctx,
		"bob",
		"render-revoke",
		`/send_message_to 101 --template --html '{{.user_informal_name}}'`,
		source,
	)
	requireCode(t, err, "source_revoked")
	require.Equal(t, 1, calls)
	var cached bool
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT overrides ? 'informal_name' FROM core.admin_broadcast_profiles WHERE owner='alice'`).
			Scan(&cached),
	)
	require.False(t, cached, "provider result revoked during call must not reach shared cache")
	var retained bool
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.admin_message_recipients WHERE content::text LIKE '%revoked generated canary%')`).
			Scan(&retained),
	)
	require.False(t, retained)
}

func TestDerivedBroadcastClaimedRetryCannotResurrectRevokedSource(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	ctx := t.Context()
	history := conversation.Service{DB: db}
	require.NoError(t, history.AppendOriginal(ctx, "bob", "claim-source", "user", "claim source"))
	var original int64
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='bob' AND source_key='claim-source'`).
			Scan(&original),
	)
	generation := int64(0)
	source := readsource.Derivation{
		Generation:     &generation,
		Authorities:    []readsource.Authority{},
		PrivateHistory: true,
	}
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	draft, err := service.PreviewDerivedCommand(
		ctx,
		"bob",
		"claim",
		`/send_message_to 101 --msg "frozen canary"`,
		source,
	)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(ctx, "bob", draft.ID))
	delivery, found, err := service.Claim(ctx)
	require.NoError(t, err)
	require.True(t, found)
	gate, err := service.BeginDelivery(ctx, deliverypolicy.Attempt{ID: delivery.ID, Generation: delivery.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(t, history.DeleteContent(ctx, "bob", original))
	requireCode(t, service.CheckPublication(ctx, "bob", draft.ID), "source_revoked")
	require.NoError(t, service.Complete(ctx, delivery.ID, delivery.Attempt, 0, "provider_unavailable", true))
	var state, content string
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT state,content::text FROM core.admin_message_deliveries WHERE id=$1`, delivery.ID).
			Scan(&state, &content),
	)
	require.Equal(t, "cancelled", state)
	require.Equal(t, "{}", content)
}
