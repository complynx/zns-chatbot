package bot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestNotificationRenderDomainFailurePreservesSQL(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	failure := core.DatabaseFailure(denial)
	_, notice, err := massageFailure(massageView{}, i18n.MassageSaved, failure)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Empty(t, notice)
	_, notice, err = massageFailure(massageView{}, i18n.MassageSaved, denial)
	require.NoError(t, err)
	require.Equal(t, i18n.MassageStale, notice)
	b := &Bot{}
	require.ErrorIs(t, b.foodFailure(t.Context(), incoming{}, failure), core.ErrDatabase)
	require.ErrorIs(t, b.foodFailure(t.Context(), incoming{}, io.EOF), io.EOF)
}

func TestRefundRenderingPreservesMarkedForbidden(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	for _, marked := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if marked {
				w.Header().Set(core.DatabaseFailureHeader, "1")
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"forbidden"}`))
		}))
		b := &Bot{DB: db, API: appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}}
		err := b.renderRefunds(t.Context(), "alice", 1, "en", map[string]bool{})
		server.Close()
		if marked {
			require.ErrorIs(t, err, core.ErrDatabase)
		} else {
			require.NoError(t, err)
		}
	}
}
