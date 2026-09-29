package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

type directOrderHTTP struct{ t *testing.T }

func (h directOrderHTTP) RoundTrip(*http.Request) (*http.Response, error) {
	h.t.Error("local order operation used HTTP")
	return nil, errors.New("unexpected HTTP")
}

func localOrderClient(t *testing.T, f *fixture) appclient.Client {
	t.Helper()
	c := f.b.API
	c.HTTP = &http.Client{Transport: directOrderHTTP{t: t}}
	c.LocalOrders = &appclient.LocalOrders{
		Service: orders.Service{DB: f.db},
		Authorizer: applicationauth.Authorizer{
			DB:     f.db,
			Verify: func(_ context.Context, token string) (string, error) { return f.b.Host.Signer.Verify(token) },
		},
	}
	return c
}

func TestDirectOrdersTypedBoundary(t *testing.T) {
	t.Parallel()
	f := setup(t)
	c := localOrderClient(t, f)
	const event = "sandbox-festival"
	choice, err := c.QuoteOrder(t.Context(), "alice", event, *orderChoice("preparty"))
	require.NoError(t, err)
	require.NotEmpty(t, choice)
	command := orders.Command{
		EventID: event,
		Name:    "create",
		Origin:  "manual",
		Key:     "direct-create",
		Choice:  orderChoice("preparty"),
	}
	order, err := c.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	replay, err := c.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Equal(t, order.ID, replay.ID)
	proof, err := c.UploadProof(t.Context(), "alice", "receipt.txt", []byte("local synthetic receipt"))
	require.NoError(t, err)
	submit := orderCommand("proof", order)
	submit.ProofFile = proof.ID
	order, err = c.ExecuteOrder(t.Context(), "alice", submit)
	require.NoError(t, err)
	for _, test := range []struct {
		name string
		read func(appclient.Client) (any, error)
	}{
		{"event", func(c appclient.Client) (any, error) { return c.OrderEvent(t.Context(), "alice", event) }},
		{"list", func(c appclient.Client) (any, error) { return c.Orders(t.Context(), "alice", event) }},
		{"page", func(c appclient.Client) (any, error) { return c.OrdersPage(t.Context(), "alice", event, "", false) }},
		{"get", func(c appclient.Client) (any, error) { return c.Order(t.Context(), "alice", event, order.ID) }},
		{"id", func(c appclient.Client) (any, error) { return c.OrderByID(t.Context(), "alice", order.ID) }},
		{"inbox", func(c appclient.Client) (any, error) { return c.PaymentInbox(t.Context(), "bob", event) }},
		{"review", func(c appclient.Client) (any, error) { return c.ReviewOrder(t.Context(), "bob", event, order.ID) }},
		{"admins", func(c appclient.Client) (any, error) { return c.PaymentAdmins(t.Context(), "alice", event) }},
		{"instructions", func(c appclient.Client) (any, error) {
			return c.PaymentInstructions(t.Context(), "alice", event, order.ID)
		}},
		{"history", func(c appclient.Client) (any, error) { return c.OrderHistory(t.Context(), "alice", event) }},
		{"history page", func(c appclient.Client) (any, error) { return c.OrderHistoryPage(t.Context(), "alice", event, "") }},
		{"event page", func(c appclient.Client) (any, error) { return c.OrderEventsPage(t.Context(), "alice", "") }},
		{"catalog page", func(c appclient.Client) (any, error) {
			return c.OrderEventChunk(t.Context(), "alice", event, "", false)
		}},
		{"proof metadata", func(c appclient.Client) (any, error) { return c.OrderProof(t.Context(), "alice", event, order.ID) }},
	} {
		actual, readErr := test.read(c)
		require.NoError(t, readErr, test.name)
		expected, httpErr := test.read(f.b.API)
		require.NoError(t, httpErr, test.name)
		actualJSON, marshalErr := json.Marshal(actual)
		require.NoError(t, marshalErr)
		expectedJSON, marshalErr := json.Marshal(expected)
		require.NoError(t, marshalErr)
		require.JSONEq(t, string(expectedJSON), string(actualJSON), test.name)
	}
	history, err := c.OrderHistoryPage(t.Context(), "alice", event, "")
	require.NoError(t, err)
	require.NotEmpty(t, history.Items)
	detail, err := c.OrderHistoryDetail(t.Context(), "alice", event, history.Items[0].ID, "")
	require.NoError(t, err)
	require.NotEmpty(t, detail.JSON)
	download, err := c.DownloadOrderProof(t.Context(), "alice", event, order.ID)
	require.NoError(t, err)
	require.Equal(t, []byte("local synthetic receipt"), download.Body)
	_, err = c.DownloadOrderProof(t.Context(), "charlie", event, order.ID)
	require.Error(t, err)
	export, err := c.ExportOrders(t.Context(), "bob", event)
	require.NoError(t, err)
	require.NotEmpty(t, export)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.order_events SET menu=menu || jsonb_build_object('direct_padding',repeat('x',1200000)) WHERE id=$1`,
		event,
	)
	require.NoError(t, err)
	large, err := c.OrderEvent(t.Context(), "alice", event)
	require.NoError(t, err)
	require.Greater(t, len(large.Menu), appclient.MaxAPIBytes)
}

type directOrderIdentity struct {
	token string
	err   error
	calls int
}

func (i *directOrderIdentity) Telegram(context.Context, int64) (identity.User, error) {
	return identity.User{Owner: "alice", Subject: "subject-alice"}, nil
}
func (i *directOrderIdentity) Exchange(context.Context, string) (string, error) {
	i.calls++
	return i.token, i.err
}

func TestDirectOrdersFreshAuthority(t *testing.T) {
	t.Parallel()
	f := setup(t)
	c := localOrderClient(t, f)
	provider := &directOrderIdentity{token: f.b.Host.Signer.Token("alice")}
	c.Links, c.Exchange = provider, provider
	ctx, owner, err := c.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	_, err = c.OrderEvent(ctx, owner, "sandbox-festival")
	require.NoError(t, err)
	outage := errors.New("exchange temporarily unavailable")
	provider.err = outage
	_, err = c.OrderEvent(ctx, owner, "sandbox-festival")
	require.ErrorIs(t, err, outage)
	provider.err = nil
	provider.token = "invalid"
	_, err = c.OrderEvent(ctx, owner, "sandbox-festival")
	requireCode(t, err, "unauthorized")
	provider.token = f.b.Host.Signer.Token("bob")
	c.LocalOrders.Service = orders.Service{}
	_, err = c.OrderEvent(ctx, owner, "sandbox-festival")
	requireCode(t, err, "forbidden")
	provider.token = f.b.Host.Signer.Token("alice")
	c.LocalOrders.Service = orders.Service{DB: f.db}
	_, err = c.OrderEvent(ctx, owner, "sandbox-festival")
	require.NoError(t, err)
	require.Equal(t, 5, provider.calls)
	c.LocalOrders.Authorizer.Verify = func(context.Context, string) (string, error) { return "missing-owner", nil }
	_, err = c.OrderEvent(ctx, owner, "sandbox-festival")
	requireCode(t, err, "forbidden")
}

func TestDirectOrdersReauthorizeEveryPage(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"identity", "inbox role"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			c := localOrderClient(t, f)
			choice, err := c.QuoteOrder(t.Context(), "alice", "sandbox-festival", *orderChoice("preparty"))
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.orders(id,event_id,owner,version,choice,state,attempt,attempt_at,payment_admin,country,created_at)
SELECT 'direct-page-'||i,'sandbox-festival','alice',1,$1,'cash','direct-attempt-'||i,clock_timestamp(),'bob','be',
'2026-01-01'::timestamptz+i*interval '1 second' FROM generate_series(1,26) i`,
				choice,
			)
			require.NoError(t, err)
			calls := 0
			c.LocalOrders.Authorizer.Verify = func(ctx context.Context, token string) (string, error) {
				calls++
				if calls == 2 {
					if scenario == "identity" {
						return "", identity.ErrZitadelUserInactive
					}
					_, revokeErr := f.db.Exec(ctx, `DELETE FROM core.order_admins WHERE owner='bob'`)
					require.NoError(t, revokeErr)
				}
				return f.b.Host.Signer.Verify(token)
			}
			var result []orders.Order
			if scenario == "identity" {
				result, err = c.Orders(t.Context(), "alice", "sandbox-festival")
				requireCode(t, err, "unauthorized")
			} else {
				result, err = c.PaymentInbox(t.Context(), "bob", "sandbox-festival")
				requireCode(t, err, "forbidden")
			}
			require.Nil(t, result, "a failed page must not expose a partial successful listing")
			require.Equal(t, 2, calls)
		})
	}
}

func TestDirectOrdersSanitizeDomainFailure(t *testing.T) {
	t.Parallel()
	f := setup(t)
	c := localOrderClient(t, f)
	_, err := f.db.Exec(t.Context(), `ALTER TABLE core.order_events RENAME TO private_database_detail`)
	require.NoError(t, err)
	_, err = c.OrderEvent(t.Context(), "alice", "sandbox-festival")
	requireCode(t, err, "internal_error")
	require.NotContains(t, err.Error(), "order_events")
	require.NotContains(t, err.Error(), "SQL")
}
