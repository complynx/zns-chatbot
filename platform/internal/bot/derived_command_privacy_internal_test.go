package bot

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedCommandReplyRechecksSourceAfterCommittedEffect(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me/history/generation":
			_, _ = w.Write([]byte(`{"generation":0}`))
		case "/internal/history/authority":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"history_stale"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	b := Bot{DB: db, API: appclient.Client{
		Base:     server.URL,
		Exchange: &authExchange{},
		Links: authLinks{
			user: identity.User{Owner: "alice", Subject: "z-alice"},
		},
		SandboxToken: (identity.Signer{}).Token,
	}}
	b.Host = appclient.Host{Base: b.API.Base, HTTP: b.API.HTTP, UserToken: b.API.UserToken}
	ctx, owner, err := b.API.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	plan := interaction.SavedPlan{FormatVersion: interaction.CurrentFormatVersion,
		Kind:         interaction.CommandPlan,
		State:        interaction.Ready,
		OrderCommand: &orders.Command{},
		PassAuthority: &interaction.PlanAuthority{
			Reads: []interaction.PassContextDependency{},
			ReadAuthorities: readsource.Registration(
				[]passbooking.ReadAuthority{{Kind: passbooking.ReadOwnedEvent, Event: "private-event"}},
			),
		},
	}
	_, err = (interaction.Store{DB: db}).SaveWinner(ctx, owner, 910, plan)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES
 ('alice',910,'registration_action','{"committed":"true"}'),
 ('alice',910,'reply','"derived private answer"'),('alice',910,'reply_origin','"agent"')`)
	require.NoError(t, err)
	visible, err := b.derivedReplyVisible(ctx, owner, 910)
	require.NoError(t, err)
	require.False(t, visible)
	var committed bool
	require.NoError(t, db.QueryRow(ctx, `SELECT content->>'committed'='true' FROM bot.interactions
 WHERE owner='alice' AND update_id=910 AND kind='registration_action'`).Scan(&committed))
	require.True(t, committed)
}
