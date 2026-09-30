package bot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestNotificationDeferralPreservesDatabaseFailure(t *testing.T) {
	t.Parallel()
	for _, domain := range []struct {
		name string
		call func(*Bot, context.Context, bool, error) error
	}{
		{"orders", func(b *Bot, ctx context.Context, followup bool, err error) error {
			return b.deferOrderNotification(ctx, orders.Notification{ID: 1, FollowupPending: followup}, err)
		}},
		{"passes", func(b *Bot, ctx context.Context, followup bool, err error) error {
			return b.deferPassNotification(ctx, passbooking.Notification{ID: 1, FollowupPending: followup}, err)
		}},
		{"massage", func(b *Bot, ctx context.Context, followup bool, err error) error {
			return b.deferMassageNotification(ctx, "alice", massage.Notice{ID: 1, FollowupPending: followup}, err)
		}},
		{"food", func(b *Bot, ctx context.Context, followup bool, err error) error {
			return b.deferFoodNotification(ctx, legacyfood.Notification{ID: 1, FollowupPending: followup}, err)
		}},
	} {
		for _, followup := range []bool{false, true} {
			t.Run(domain.name+"/followup="+strconv.FormatBool(followup), func(t *testing.T) {
				t.Parallel()
				var completions atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					completions.Add(1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ok":true}`))
				}))
				t.Cleanup(server.Close)
				b := &Bot{Host: appclient.Host{Base: server.URL}}
				err := domain.call(b, t.Context(), followup, core.ErrDatabase)
				require.ErrorIs(t, err, core.ErrDatabase)
				require.Zero(t, completions.Load(), "SQL failure must not complete or defer delivery")
				require.NoError(t, domain.call(b, t.Context(), followup, io.EOF))
				require.EqualValues(t, 1, completions.Load(), "ordinary provider failure retains completion policy")
			})
		}
	}
}
