package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestAdminAuthorizationDatabaseFailureDoesNotComplete(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: "source_revoked"}
	for _, test := range []struct {
		name    string
		failure error
		want    error
		outcome delivery.Kind
	}{
		{"SQL unavailable", core.ErrDatabase, core.ErrDatabase, ""},
		{"SQL denial", core.DatabaseFailure(denial), core.ErrDatabase, ""},
		{"domain denial", denial, nil, delivery.Cancelled},
		{"provider unavailable", io.EOF, io.EOF, delivery.Deferred},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			completed := make(chan adminmessage.Completion, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var value adminmessage.Completion
				if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				completed <- value
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(server.Close)
			b := &Bot{Host: appclient.Host{Base: server.URL}}
			err := b.finishAdminAuthorizationFailure(
				t.Context(),
				adminmessage.Delivery{ID: 7, Attempt: 2},
				test.failure,
			)
			if test.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.want)
			}
			if test.outcome == "" {
				require.Empty(t, completed, "SQL failure must not finish the delivery")
			} else {
				require.Len(t, completed, 1)
				value := <-completed
				require.EqualValues(t, 7, value.ID)
				require.EqualValues(t, 2, value.Attempt)
				require.Equal(t, test.outcome, value.Outcome.Kind)
			}
		})
	}
}
