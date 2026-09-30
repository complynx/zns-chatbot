package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Faults stop the caller at external boundaries without modifying product code.
type orderRestartTransport struct {
	window   string
	armed    bool
	commands [][]byte
}

func (f *orderRestartTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost ||
		(r.URL.Path != "/v1/order-actions" && r.URL.Path != "/internal/derived/order-actions" && r.URL.Path != "/internal/derived/order-actions/receipt") {
		if f.armed && f.window == "delivery" {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body: io.NopCloser(bytes.NewBufferString(
					`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`,
				)),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if err = r.Body.Close(); err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	command, err := restartOrderCommand(r.URL.Path, body)
	if err != nil {
		return nil, err
	}
	if r.URL.Path == "/internal/derived/order-actions/receipt" {
		return f.receipt(r, command)
	}
	f.commands = append(f.commands, command)
	if f.armed && f.window == "saved" {
		return nil, errors.New("order test stopped after plan save")
	}
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if f.armed && f.window == "committed" && response.StatusCode == http.StatusOK {
		if err = response.Body.Close(); err != nil {
			return nil, err
		}
		return nil, errors.New("order test response lost after commit")
	}
	return response, nil
}

func boundOrderFixture(t *testing.T) (*fixture, orders.Order) {
	t.Helper()
	f := setup(t)
	order, err := (orders.Service{DB: f.db}).Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "restart-seed",
		Choice:  &orders.ChoiceInput{},
	})
	require.NoError(t, err)
	f.model.plan = agent.Plan{View: agent.OrdersView, OrderAction: &agent.OrderProposal{
		Name: "add_extra", OrderID: order.ID, Extra: "preparty",
	}}
	return f, order
}

func savedOrderPlan(t *testing.T, f *fixture) string {
	t.Helper()
	var plan string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT payload::text FROM interaction.saved_turns WHERE owner='alice' AND update_id=88001`).
			Scan(&plan),
	)
	return plan
}

func restartOrderBot(f *fixture) *recordingModel {
	// A new Bot and a different model make persisted replay observable. A repeated
	// model call would propose a new order instead of the originally bound edit.
	model := &recordingModel{
		plan: agent.Plan{View: agent.OrdersView, OrderAction: &agent.OrderProposal{Name: "create"}},
	}
	f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: model, Delivery: f.b.Delivery}
	return model
}

func orderReceiptCount(t *testing.T, f *fixture) int {
	t.Helper()
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_operations WHERE actor='alice' AND key='tg-order-88001'`).
			Scan(&count),
	)
	return count
}

func TestOrderInteractionRestartsAtDurableBoundaries(t *testing.T) {
	t.Parallel()
	for _, window := range []string{"saved", "committed", "delivery"} {
		t.Run(window, func(t *testing.T) {
			t.Parallel()
			f, original := boundOrderFixture(t)
			wire := &orderRestartTransport{window: window, armed: window != "delivery"}
			f.b.API.HTTP = &http.Client{Transport: wire}
			f.b.Host.HTTP = f.b.API.HTTP
			delivery := &orderRestartTransport{window: "delivery", armed: window == "delivery"}
			f.b.TG.HTTP = &http.Client{Transport: delivery}
			update := message(88001, 101, "add preparty to order "+original.ID)
			if window == "delivery" {
				require.NoError(t, f.b.Handle(t.Context(), update))
				pumpBotDeliveries(t, f.b)
				var deferred int
				require.NoError(
					t,
					f.db.QueryRow(
						t.Context(),
						`SELECT count(*) FROM bot.delivery_intents WHERE owner='alice' AND state='pending' AND attempt=1 AND not_before>clock_timestamp()`,
					).Scan(&deferred),
				)
				require.Positive(t, deferred, "provider retry must remain durable after admission succeeds")
			} else {
				require.Error(t, f.b.Handle(t.Context(), update))
			}
			require.Equal(t, 1, f.model.calls)
			before := savedOrderPlan(t, f)
			var bound struct {
				Command orders.Command `json:"order_command"`
			}
			require.NoError(t, json.Unmarshal([]byte(before), &bound))
			require.Equal(t, original.ID, bound.Command.OrderID)
			require.Equal(t, original.Version, bound.Command.Version)
			require.Len(t, wire.commands, 1)
			bound.Command.Key = "tg-order-88001"
			expected, err := json.Marshal(bound.Command)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(wire.commands[0]))
			expectedCommits := 1
			if window == "saved" {
				expectedCommits = 0
			}
			assert.Equal(t, expectedCommits, orderReceiptCount(t, f))
			var replies int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE update_id=88001 AND kind='orders_reply'`).
					Scan(&replies),
			)
			if window == "delivery" {
				assert.Equal(t, 1, replies)
			} else {
				assert.Zero(t, replies)
			}
			model := restartOrderBot(f)
			wire.armed, delivery.armed = false, false
			require.NoError(t, f.b.Handle(t.Context(), update))
			if window == "delivery" {
				_, resetErr := f.db.Exec(
					t.Context(),
					`UPDATE bot.delivery_intents SET not_before=clock_timestamp()-interval '1 second' WHERE owner='alice' AND state='pending';
				UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second';
				UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
				)
				require.NoError(t, resetErr)
			}
			pumpBotDeliveries(t, f.b)
			assert.Zero(t, model.calls)
			assert.Equal(t, before, savedOrderPlan(t, f), "restart must use the saved winner unchanged")
			require.Len(t, wire.commands, 2)
			assert.Equal(t, wire.commands[0], wire.commands[1], "exact command and operation key survive restart")
			assert.Equal(t, 1, orderReceiptCount(t, f))
			var hash string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT request_hash FROM core.order_operations WHERE actor='alice' AND key='tg-order-88001'`).
					Scan(&hash),
			)
			digest := sha256.Sum256(expected)
			assert.Equal(t, hex.EncodeToString(digest[:]), hash)
			current, err := (orders.Service{DB: f.db}).Get(t.Context(), "alice", original.EventID, original.ID)
			require.NoError(t, err)
			assert.Equal(t, original.Version+1, current.Version, "exactly one authoritative edit")
			assert.Contains(t, current.Choice.Extras, "preparty")
			assert.Contains(t, replyVisibilityState(t, f), "Препати")
		})
	}
}

func TestOrderSavedPlanRechecksResumeFences(t *testing.T) {
	t.Parallel()
	for _, fence := range []string{"version", "permission", "history"} {
		t.Run(fence, func(t *testing.T) {
			t.Parallel()
			f, original := boundOrderFixture(t)
			wire := &orderRestartTransport{window: "saved", armed: true}
			f.b.API.HTTP = &http.Client{Transport: wire}
			f.b.Host.HTTP = f.b.API.HTTP
			update := message(88001, 101, "add preparty to order "+original.ID)
			require.Error(t, f.b.Handle(t.Context(), update))
			before := savedOrderPlan(t, f)
			service := orders.Service{DB: f.db}
			switch fence {
			case "version":
				_, err := service.Execute(
					t.Context(),
					"alice",
					orders.Command{
						EventID: original.EventID,
						OrderID: original.ID,
						Name:    "edit",
						Origin:  "manual",
						Key:     "intervening-edit",
						Version: original.Version,
						Choice:  &orders.ChoiceInput{Customer: "later manual name"},
					},
				)
				require.NoError(t, err)
			case "permission":
				_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
				require.NoError(t, err)
			case "history":
				var event int64
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='alice' AND kind='user' ORDER BY id DESC LIMIT 1`).
						Scan(&event),
				)
				require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", event))
			}
			model := restartOrderBot(f)
			wire.armed = false
			err := f.b.Handle(t.Context(), update)
			if fence == "history" {
				require.ErrorContains(t, err, "terminal history plan")
				var terminal bool
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT (kind='terminal' AND state='privacy_terminal' AND reason='history_deleted') FROM interaction.saved_turns WHERE owner='alice' AND update_id=88001`).
						Scan(&terminal),
				)
				assert.True(t, terminal)
				assert.Len(t, wire.commands, 1, "deleted plan cannot issue another command")
			} else {
				if fence == "version" {
					require.NoError(t, err)
				}
				var code string
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT content->>'code' FROM bot.interactions WHERE update_id=88001 AND kind='order_error'`).
						Scan(&code),
				)
				expected := "stale_version"
				if fence == "permission" {
					expected = "forbidden"
				}
				assert.Equal(t, expected, code)
				assert.Equal(t, before, savedOrderPlan(t, f))
				require.Len(t, wire.commands, 2)
				assert.Equal(t, wire.commands[0], wire.commands[1])
			}
			assert.Zero(t, model.calls)
			assert.Equal(t, 1, f.model.calls)
			assert.Zero(t, orderReceiptCount(t, f))
			var hasExtra bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT choice->'extras' ? 'preparty' FROM core.orders WHERE id=$1`, original.ID).
					Scan(&hasExtra),
			)
			assert.False(t, hasExtra)
		})
	}
}

func restartOrderCommand(path string, body []byte) ([]byte, error) {
	command := body
	if path == "/internal/derived/order-actions" || path == "/internal/derived/order-actions/receipt" {
		var envelope struct {
			Command json.RawMessage       `json:"command"`
			Source  readsource.Derivation `json:"source"`
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&envelope); err != nil {
			return nil, err
		}
		if !envelope.Source.Valid() || len(envelope.Command) == 0 || bytes.Equal(envelope.Command, []byte("null")) {
			return nil, errors.New("invalid derived order test envelope")
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, errors.New("trailing derived order test envelope")
		}
		command = envelope.Command
	}
	return command, nil
}

// Absent probes do not attempt a mutation. A found receipt or current denial is
// the authoritative retry outcome and must carry the exact original command.
func (f *orderRestartTransport) receipt(r *http.Request, command []byte) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	if err = response.Body.Close(); err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	var result struct {
		Found bool `json:"found"`
	}
	if response.StatusCode == http.StatusOK {
		if err = json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
	}
	if result.Found || response.StatusCode == http.StatusForbidden {
		f.commands = append(f.commands, command)
	}
	return response, nil
}
