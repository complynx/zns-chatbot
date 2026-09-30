package bot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestR33PassMenuMappersKeepPositiveSQL(t *testing.T) {
	t.Parallel()
	stale := &core.ProblemError{Status: http.StatusConflict, Code: passSourceStale}
	cases := map[string]error{
		"marked domain":       core.DatabaseFailure(stale),
		"marker and cancel":   errors.Join(core.DatabaseOperationError(io.EOF), context.Canceled),
		"marker cancel stale": errors.Join(core.DatabaseOperationError(io.EOF), context.Canceled, stale),
		"serialization":       core.ErrDatabaseSerialization,
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mapped := passMenuFailure(err)
			requireSanitizedDatabaseError(t, mapped)
			_, domain := errors.AsType[*core.ProblemError](mapped)
			require.False(t, domain)
			require.False(t, stalePassMenuSource(err))
		})
	}
}

func TestR33PassMenuMappersKeepOrdinaryOutcomes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		err        error
		suppressed bool
		stale      bool
	}{
		"source stale": {
			err: &core.ProblemError{Status: http.StatusConflict, Code: passSourceStale}, suppressed: true, stale: true,
		},
		"pass source stale": {
			err: &core.ProblemError{
				Status: http.StatusConflict,
				Code:   "pass_source_stale",
			},
			suppressed: true,
			stale:      true,
		},
		"history stale": {
			err: &core.ProblemError{Status: http.StatusConflict, Code: historyStale}, suppressed: true, stale: true,
		},
		"other client problem": {
			err: &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}, suppressed: true,
		},
		"server problem": {
			err: &core.ProblemError{Status: http.StatusBadGateway, Code: "upstream"},
		},
		"provider eof": {
			err: io.EOF,
		},
		"cancellation": {
			err: context.Canceled,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mapped := passMenuFailure(tc.err)
			if tc.suppressed {
				require.NoError(t, mapped)
			} else {
				require.Equal(t, tc.err, mapped)
				require.False(t, core.IsDatabaseFailure(mapped))
			}
			require.Equal(t, tc.stale, stalePassMenuSource(tc.err))
		})
	}
}
