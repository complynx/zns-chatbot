package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type modernRevokeTransport struct{ before func(context.Context) error }

func (tr modernRevokeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v1/order-actions" || r.URL.Path == "/internal/derived/order-actions" {
		if err := tr.before(r.Context()); err != nil {
			return nil, err
		}
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestModernOrdersTransactionRechecksAdminAndBooking(t *testing.T) {
	t.Parallel()
	for _, revoke := range []string{"role", "booking"} {
		t.Run(revoke, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			s := orders.Service{DB: f.db}
			order, err := s.Execute(
				t.Context(),
				"alice",
				orders.Command{
					EventID: "sandbox-festival",
					Name:    "create",
					Origin:  "manual",
					Key:     "create",
					Choice:  orderChoice("preparty"),
				},
			)
			require.NoError(t, err)
			command := orderCommand("cash", order)
			command.PaymentAdmin = "bob"
			order, err = s.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			f.b.API.HTTP = &http.Client{Transport: modernRevokeTransport{before: func(ctx context.Context) error {
				query := `DELETE FROM core.order_admins WHERE owner='bob'`
				if revoke == "booking" {
					query = `UPDATE core.users SET can_book=false WHERE id='bob'`
				}
				_, execErr := f.db.Exec(ctx, query)
				return execErr
			}}}
			f.b.Host.HTTP = f.b.API.HTTP
			result := runModernVM(t, f, 38401, identity.BobTelegramID, "Accept "+order.ID, fmt.Sprintf(`
tools.orders.review.read({order_id:%q});let denied=false;try{tools.orders.review.decide({order_id:%q,name:"accept"});}catch(_){denied=true;}
let help=false;try{tools.orders.review.decide.$help();help=true;}catch(_){} return {denied,help,listed:tools.$list().some(x=>x.name==="orders.review.decide")};`, order.ID, order.ID))
			assert.JSONEq(t, `{"denied":true,"help":false,"listed":false}`, string(result))
			current, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, "cash", current.State)
		})
	}
}

func TestModernOrdersReadRequiresCompleteSequentialChunks(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	order.Choice.Customer = strings.Repeat("Ж", 9000)
	_, err = f.db.Exec(t.Context(), `UPDATE core.orders SET choice=$2 WHERE id=$1`, order.ID, order.Choice)
	require.NoError(t, err)
	forged := core.EncodeReadCursor(
		core.ReadCursor{Actor: "alice", Scope: "orders.inspect:sandbox-festival:" + order.ID, Offset: 9000},
	)
	result := runModernVM(t, f, 38501, identity.AliceTelegramID, "Delete "+order.ID, fmt.Sprintf(`
let forged=false;try{tools.orders.inspect({order_id:%q,cursor:%q});forged=true;}catch(_){}
const first=tools.orders.inspect({order_id:%q});let partial=false;try{tools.orders.update({name:"delete",order_id:%q});partial=true;}catch(_){}
let cursor=first.next_cursor,whole=first.json;while(cursor){const p=tools.orders.inspect({order_id:%q,cursor});cursor=p.next_cursor;whole+=p.json;}
const full=JSON.parse(whole);const deleted=tools.orders.update({name:"delete",order_id:%q});return {forged,partial,length:full.choice.customer.length,state:deleted.state};`, order.ID, forged, order.ID, order.ID, order.ID, order.ID))
	assert.JSONEq(t, `{"forged":false,"partial":false,"length":9000,"state":"deleted"}`, string(result))
}

type modernLostDocument struct{ sent int }

func (tr *modernLostDocument) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if err == nil && strings.HasSuffix(r.URL.Path, "/sendDocument") {
		tr.sent++
		_ = response.Body.Close()
		return nil, errors.New("simulated lost document acknowledgement")
	}
	return response, err
}

func TestModernOrdersExportUncertaintyAndRestart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	transport := &modernLostDocument{}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	result := runModernVM(
		t,
		f,
		38601,
		identity.BobTelegramID,
		"Export modern orders",
		`const first=tools.orders.export({});const second=tools.orders.export({});return {first:first.status,second:second.status};`,
	)
	assert.JSONEq(t, `{"first":"uncertain","second":"uncertain"}`, string(result))
	assert.Equal(t, 1, transport.sent)
	documents := exportDocuments(t, f, identity.BobTelegramID)
	require.Len(t, documents, 1)
	body, err := f.b.TG.Download(t.Context(), telegram.Document{FileID: documents[0], Filename: "orders.xlsx"})
	require.NoError(t, err)
	digest := sha256.Sum256(body)
	var receipt struct {
		SHA256 string `json:"sha256"`
		Bytes  int    `json:"bytes"`
	}
	err = f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=38601 AND kind='modern_order_delivery:orders.export'`).
		Scan(&receipt)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(digest[:]), receipt.SHA256)
	assert.Equal(t, len(body), receipt.Bytes)
	f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: f.b.Model}
	handle(t, f.b, message(38601, identity.BobTelegramID, "Export modern orders"))
	assert.Equal(t, 1, transport.sent)
}

func TestModernOrdersRejectForgedAuthorityAndForeignTargets(t *testing.T) {
	t.Parallel()
	f := setup(t)
	s := orders.Service{DB: f.db}
	order, err := s.Execute(
		t.Context(),
		"bob",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "foreign",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	result := runModernVM(t, f, 38701, identity.AliceTelegramID, "Show my order", fmt.Sprintf(`
let denied=0;for(const args of [{name:"create",choice:{},owner:"bob"},{name:"create",choice:{},version:0},{name:"create",choice:{},attempt:"forged"},{name:"proof",proof_file:"arbitrary"}]){try{tools.orders.update(args);}catch(_){denied++;}}
try{tools.orders.inspect({order_id:%q});}catch(_){denied++;} return {denied};`, order.ID))
	assert.JSONEq(t, `{"denied":5}`, string(result))
	list, err := s.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.Empty(t, list)
}
