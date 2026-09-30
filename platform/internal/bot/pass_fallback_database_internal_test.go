package bot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestR35RegistrationProfileFailureYieldsToSQL(t *testing.T) {
	t.Parallel()
	required := &core.ProblemError{Status: http.StatusConflict, Code: "pass_profile_required"}
	for name, test := range map[string]struct {
		err     error
		profile bool
	}{
		"ordinary profile refusal":  {err: required, profile: true},
		"ordinary unrelated code":   {err: &core.ProblemError{Status: http.StatusConflict, Code: "stale"}},
		"marked profile refusal":    {err: core.DatabaseFailure(required)},
		"marker joined with cancel": {err: errors.Join(core.DatabaseFailure(required), context.Canceled)},
		"plain safe sql":            {err: core.ErrDatabase},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.profile, registrationProfileFailure(test.err))
			if core.IsDatabaseFailure(test.err) {
				// applyPassMenu's next consumer: the stale-notice mapper stays fatal.
				requireR33NoDomainMarker(t, passMenuFailure(test.err))
			}
		})
	}
}

func TestR35PassBatchErrorPositiveSQLWins(t *testing.T) {
	t.Parallel()
	for name, positive := range r35PositiveErrors() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, dials := r35Bot(t, appclient.Client{})
			in, _ := r35Message("/passes_assign")
			requireR33NoDomainMarker(t, b.passBatchError(t.Context(), in, "en", positive))
			require.Zero(t, dials.Load(), "no batch error notice may be queued")
		})
	}
}

func TestR35PassBatchErrorOrdinaryFailuresKeepCause(t *testing.T) {
	t.Parallel()
	for name, ordinary := range map[string]error{
		"server status": &core.ProblemError{Status: http.StatusInternalServerError, Code: "internal_error"},
		"provider eof":  io.EOF,
		"cancellation":  context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, dials := r35Bot(t, appclient.Client{})
			in, _ := r35Message("/passes_assign")
			out := b.passBatchError(t.Context(), in, "en", ordinary)
			require.Equal(t, ordinary, out)
			require.False(t, core.IsDatabaseFailure(out))
			require.Zero(t, dials.Load())
		})
	}
}
