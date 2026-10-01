package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestBotDeliveryChildIdentitySurvivesRetry(t *testing.T) {
	t.Parallel()
	ref := botdelivery.Reference{Family: "massage", CardKey: "massage"}
	ctx := withBotDeliveryOrigin(
		context.Background(),
		delivery.Reference{Owner: delivery.Massage, Key: "42", Effect: "refresh"},
	)
	first, effect, child, err := botDeliveryChild(ctx, ref)
	require.NoError(t, err)
	require.True(t, child)
	ref.Revision = 9
	ref.Update = 101
	second, repeated, _, err := botDeliveryChild(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, effect, repeated)
	next := withBotDeliveryOrigin(
		context.Background(),
		delivery.Reference{Owner: delivery.Massage, Key: "43", Effect: "refresh"},
	)
	different, _, _, err := botDeliveryChild(next, ref)
	require.NoError(t, err)
	require.NotEqual(t, first, different)
	ref.CardKey = "second"
	_, differentEffect, _, err := botDeliveryChild(ctx, ref)
	require.NoError(t, err)
	require.NotEqual(t, effect, differentEffect)
}

func TestBotEditFallbackRequiresDefiniteMissingTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body string
		status     int
		kind       delivery.Kind
		fallback   bool
	}{
		{
			"missing",
			`{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`,
			400,
			delivery.Deferred,
			true,
		},
		{
			"unchanged",
			`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`,
			400,
			delivery.Succeeded,
			false,
		},
		{
			"rate_limit",
			`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":9}}`,
			429,
			delivery.Deferred,
			false,
		},
		{"lost", `not-json`, 502, delivery.Uncertain, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Contains(t, r.URL.Path, "editMessageText")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			b := Bot{TG: telegram.Client{Base: server.URL, Token: "test", HTTP: server.Client()}}
			result := b.sendBotIntent(
				context.Background(),
				botdelivery.Intent{Phase: "edit", Chat: 1, Target: 7},
				botRenderedDelivery{Payload: telegram.Send{Text: "current"}},
			)
			require.Equal(t, tt.kind, result.Kind)
			require.Equal(t, tt.fallback, result.Fallback)
			require.Equal(t, 1, requests, "an edit result must never issue its own fallback send")
			if tt.kind == delivery.Succeeded {
				require.Equal(t, int64(7), result.MessageID)
			}
		})
	}
}

func TestBotRedactionNeverFallsBackToNewMessage(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`))
	}))
	defer server.Close()
	b := Bot{TG: telegram.Client{Base: server.URL, Token: "test", HTTP: server.Client()}}
	result := b.sendBotIntent(
		context.Background(),
		botdelivery.Intent{
			Phase:     "edit",
			Chat:      1,
			Target:    7,
			Reference: botdelivery.Reference{Family: "pass_redaction"},
		},
		botRenderedDelivery{Payload: telegram.Send{Text: "unavailable"}},
	)
	require.Equal(t, delivery.Rejected, result.Kind)
	require.False(t, result.Fallback)
}

// Persisted child identity and continuation bytes must survive lint refactors.
func TestBotDeliverySerializationCompatibility(t *testing.T) {
	t.Parallel()
	ctx := withBotDeliveryOrigin(t.Context(), delivery.Reference{Owner: delivery.Massage, Key: "42", Effect: "refresh"})
	operation, _, child, err := botDeliveryChild(ctx, botdelivery.Reference{Family: "massage", CardKey: "massage"})
	require.NoError(t, err)
	require.True(t, child)
	require.Equal(t, "followup:11a50291901d18ba26d21f7276f0bebdef5abf0e3ca34da38817283b7af1cf6c", operation)
	raw, err := json.Marshal(botdelivery.Reference{Kind: botdelivery.CardIntent})
	require.NoError(t, err)
	// Byte order is part of the persisted identity; semantic JSON equality is insufficient.
	require.True(
		t,
		bytes.Equal([]byte(`{"kind":"card","continuation":{}}`), raw),
		"canonical JSON bytes changed: %s",
		raw,
	)
	raw, err = json.Marshal(
		botdelivery.Reference{
			Kind: botdelivery.DocumentIntent,
			Continuation: botdelivery.Continuation{
				Kind:     "document",
				Document: &botdelivery.DocumentReceipt{Filename: "orders.xlsx", SHA256: "digest", Bytes: 12},
			},
		},
	)
	require.NoError(t, err)
	require.True(
		t,
		bytes.Equal(
			[]byte(
				`{"kind":"document","continuation":{"document":{"filename":"orders.xlsx","sha256":"digest","bytes":12},"kind":"document"}}`,
			),
			raw,
		),
		"canonical JSON bytes changed: %s",
		raw,
	)
}
