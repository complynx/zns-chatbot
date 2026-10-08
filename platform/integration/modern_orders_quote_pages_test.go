package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func pagedModernQuoteDraft(t *testing.T, f *fixture) (orders.Order, string) {
	t.Helper()
	key := strings.Repeat("Ж<&", 1700)
	_, err := f.db.Exec(t.Context(), `UPDATE core.order_events SET extras=$1 WHERE id='sandbox-festival'`,
		map[string]orders.Extra{key: {Price: 100}})
	require.NoError(t, err)
	order, err := (orders.Service{DB: f.db}).Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "paged-quote",
		Choice: &orders.ChoiceInput{Extras: map[string]json.RawMessage{key: json.RawMessage(`0`)}},
	})
	require.NoError(t, err)
	result := runModernContinuation(t, f, 52000, identity.AliceTelegramID, "Prepare quote", fmt.Sprintf(
		`let p=tools.orders.inspect({order_id:%q});for(let i=1;i<8&&p.more;i++)p=tools.orders.inspect({order_id:%q,cursor:p.next_cursor});return tools.orders.choice({operation:"begin",order_id:%q});`,
		order.ID,
		order.ID,
		order.ID,
	))
	var draft struct {
		Ref string `json:"choice_ref"`
	}
	require.NoError(t, json.Unmarshal(result, &draft))
	require.NotEmpty(t, draft.Ref)
	return order, draft.Ref
}

func TestModernReferencedQuotePagesAcrossHostRestart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, ref := pagedModernQuoteDraft(t, f)
	cursor := ""
	var assembled strings.Builder
	for turn := range 8 {
		f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Delivery: f.b.Delivery}
		update := 52001 + int64(turn)
		runModernContinuation(t, f, update, identity.AliceTelegramID, "Read quote", fmt.Sprintf(
			`const p=tools.orders.quote({choice_ref:%q,cursor:%q});return {more:p.more,next_cursor:p.next_cursor};`,
			ref,
			cursor,
		))
		page := committedModernQuotePage(t, f, update)
		require.LessOrEqual(t, len([]rune(page.JSON)), 4000)
		assembled.WriteString(page.JSON)
		cursor = page.NextCursor
		if !page.More {
			break
		}
	}
	require.Empty(t, cursor, "complete quote must remain reachable")
	var choice orders.Choice
	require.NoError(t, json.Unmarshal([]byte(assembled.String()), &choice))
	require.Equal(t, order.Choice, choice)
}

func committedModernQuotePage(t *testing.T, f *fixture, update int64) core.ReadChunk {
	t.Helper()
	var records []struct {
		Calls []struct {
			Outcome struct {
				Name   string          `json:"name"`
				Result json.RawMessage `json:"result"`
			} `json:"outcome"`
		} `json:"calls"`
	}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, update).Scan(&records))
	for _, record := range records {
		for _, call := range record.Calls {
			if call.Outcome.Name == "orders.quote" {
				var page core.ReadChunk
				require.NoError(t, json.Unmarshal(call.Outcome.Result, &page))
				return page
			}
		}
	}
	t.Fatal("completed quote page missing")
	return core.ReadChunk{}
}

type modernQuoteRevocationTransport struct {
	base   http.RoundTripper
	revoke func()
	done   atomic.Bool
}

func (transport *modernQuoteRevocationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err == nil && strings.HasSuffix(request.URL.Path, "/choice-snapshot") &&
		transport.done.CompareAndSwap(false, true) {
		transport.revoke()
	}
	return response, err
}

func TestModernReferencedQuoteRevocationDuringPreparation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, ref := pagedModernQuoteDraft(t, f)
	client := f.b.API.HTTPClient()
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}
	client.Transport = &modernQuoteRevocationTransport{base: client.Transport, revoke: func() {
		_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
		require.NoError(t, err)
	}}
	f.b.API.HTTP = &client
	assertModernBoundaryDenied(t, f, 52010, identity.AliceTelegramID, fmt.Sprintf(
		`tools.orders.quote({choice_ref:%q});`, ref))
}
