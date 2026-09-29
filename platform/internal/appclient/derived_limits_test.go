package appclient_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedCommandBoundsMatchAcrossTransports(t *testing.T) {
	t.Parallel()
	for _, local := range []bool{false, true} {
		name := "http"
		if local {
			name = "local"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			credentialFailure := errors.New("credential boundary reached")
			host := appclient.Host{UserToken: func(context.Context, string) (string, error) {
				return "", credentialFailure
			}}
			if local {
				host.LocalDerived = &appclient.LocalDerived{}
			}
			generation := int64(0)
			source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
			// Orders permit larger saved choices than ordinary workflow commands.
			_, err := host.ExecuteDerivedOrder(
				t.Context(),
				"alice",
				orders.Command{CatalogSnapshot: strings.Repeat("x", 80<<10)},
				source,
			)
			require.ErrorIs(t, err, credentialFailure)
			_, err = host.ExecuteDerivedOrder(
				t.Context(),
				"alice",
				orders.Command{CatalogSnapshot: strings.Repeat("x", 320<<10)},
				source,
			)
			var problem *core.ProblemError
			require.ErrorAs(t, err, &problem)
			require.Equal(t, http.StatusBadRequest, problem.Status)
			_, err = host.ExecuteDerivedWorkflow(
				t.Context(),
				"alice",
				workflow.Action{Key: strings.Repeat("x", 64<<10)},
				source,
			)
			require.ErrorAs(t, err, &problem)
			require.Equal(t, http.StatusBadRequest, problem.Status)
			_, err = host.ExecuteDerivedWorkflow(t.Context(), "alice", workflow.Action{}, source)
			require.ErrorIs(t, err, credentialFailure)
		})
	}
}
