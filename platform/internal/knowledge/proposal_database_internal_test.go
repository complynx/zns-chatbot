package knowledge

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestProposalScanDatabaseFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		failure error
		want    error
	}{
		{"transport", io.EOF, core.ErrDatabase},
		{"missing", pgx.ErrNoRows, pgx.ErrNoRows},
		{"cancel", context.Canceled, context.Canceled},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := scanProposal(r29ScannedRow{row: r29Row{err: test.failure}})
			require.Equal(t, test.want, err)
		})
	}
}
