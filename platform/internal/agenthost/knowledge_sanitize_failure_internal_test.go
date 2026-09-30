package agenthost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type knowledgeSanitizeFailureDomain struct {
	KnowledgeDomain

	proposalError error
	afterProposal context.CancelFunc
	calls         []string
}

func (d *knowledgeSanitizeFailureDomain) MemoryDeletions(
	context.Context,
	string,
) (knowledge.MemoryDeletionState, error) {
	d.calls = append(d.calls, "deletions")
	return knowledge.MemoryDeletionState{}, nil
}

func (d *knowledgeSanitizeFailureDomain) KnowledgeProposals(
	_ context.Context, _ string, query knowledge.ProposalQuery,
) ([]knowledge.Proposal, error) {
	d.calls = append(d.calls, query.Event)
	if d.afterProposal != nil {
		d.afterProposal()
	}
	return []knowledge.Proposal{{Text: "fresh result must not replace bounded projection"}}, d.proposalError
}

func TestSanitizeKnowledgeReadsFailurePrecedence(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"pure denial", denial, nil},
		{"wrapped denial", fmt.Errorf("review: %w", denial), nil},
		{"database first", errors.Join(core.ErrDatabase, denial), core.ErrDatabase},
		{"denial first", errors.Join(denial, core.ErrDatabase), core.ErrDatabase},
		{"marked denial", core.DatabaseFailure(denial), core.ErrDatabase},
		{"driver statement", errors.Join(denial, &pgconn.PgError{Code: "23505", Message: "private SQL"}), core.ErrDatabase},
		{"driver connection", errors.Join(denial, knowledgeSanitizeConnectionFailure(t, io.EOF)), core.ErrDatabase},
		{"wrapped database", fmt.Errorf("review: %w", errors.Join(denial, core.ErrDatabase)), core.ErrDatabase},
		{"database and cancellation", errors.Join(denial, context.Canceled, core.ErrDatabase), core.ErrDatabase},
		{"serialization and denial", errors.Join(denial, core.ErrDatabaseSerialization), core.ErrDatabaseSerialization},
		{"serialization driver", errors.Join(denial, &pgconn.PgError{Code: "40001", Message: "private SQL"}), core.ErrDatabaseSerialization},
		{"joined cancellation", errors.Join(denial, context.Canceled), context.Canceled},
		{"joined deadline", errors.Join(context.DeadlineExceeded, denial), context.DeadlineExceeded},
		{"wrapped cancellation", fmt.Errorf("review: %w", errors.Join(denial, context.Canceled)), context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			domain := &knowledgeSanitizeFailureDomain{proposalError: test.err}
			if errors.Is(test.want, core.ErrDatabase) {
				domain.afterProposal = cancel
			}
			reader := KnowledgeReader{Domain: domain}
			original := knowledgeSanitizeFailureRead("first")
			value := &agent.KnowledgeContext{
				Reads:     []agent.KnowledgeReadResult{original, knowledgeSanitizeFailureRead("second")},
				Remaining: 0, Omitted: true,
			}
			err := reader.SanitizeKnowledgeReads(ctx, "owner", value)
			require.Zero(t, value.Remaining, "sanitization never refunds read slots")
			require.True(t, value.Omitted)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
				require.Equal(t, original, value.Reads[0], "a failed current read must not become a denial")
				require.Equal(t, []string{"deletions", "first"}, domain.calls, "failure stops further reads")
				if errors.Is(test.want, core.ErrDatabase) {
					require.Equal(t, test.want, err, "only the sanitized database marker leaves the reader")
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"deletions", "first", "second"}, domain.calls)
			for _, read := range value.Reads {
				require.Equal(t, agent.KnowledgeReadResult{
					Request: read.Request, Error: "forbidden", Omitted: true,
				}, read)
			}
			require.Equal(t, original.Request, value.Reads[0].Request)
		})
	}
}

func TestSanitizeKnowledgeReadsSuccessfulResultPreservesCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	domain := &knowledgeSanitizeFailureDomain{afterProposal: cancel}
	reader := KnowledgeReader{Domain: domain}
	original := knowledgeSanitizeFailureRead("first")
	value := &agent.KnowledgeContext{Reads: []agent.KnowledgeReadResult{original}, Remaining: 1}
	require.ErrorIs(t, reader.SanitizeKnowledgeReads(ctx, "owner", value), context.Canceled)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, original, value.Reads[0])
	require.Equal(t, 1, value.Remaining)
	require.Equal(t, []string{"deletions", "first"}, domain.calls)
}

func knowledgeSanitizeFailureRead(event string) agent.KnowledgeReadResult {
	return agent.KnowledgeReadResult{
		Request:   agent.KnowledgeProposal{Name: agent.KnowledgeProposals, Event: event, ReviewQueue: true},
		Proposals: []knowledge.Proposal{{Text: "retained bounded projection"}},
		Facts:     []knowledge.Fact{{Text: "retained private fact"}},
		Memo:      &knowledge.Memo{Text: "retained private memo"},
		More:      true, NextCursor: "retained-cursor", Omitted: true,
	}
}

func TestSanitizeKnowledgeReadsJoinedDriverCancellation(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	for _, driver := range []struct {
		name string
		code string
		want error
	}{
		{"statement", "23505", core.ErrDatabase},
		{"serialization", "40001", core.ErrDatabaseSerialization},
		{"connection", "", core.ErrDatabase},
	} {
		for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
			for _, order := range []struct{ callerCanceled, driverFirst bool }{
				{false, false}, {false, true}, {true, false}, {true, true},
			} {
				callerCanceled, driverFirst := order.callerCanceled, order.driverFirst
				t.Run(fmt.Sprintf("%s/%s/callerCanceled=%t/driverFirst=%t", driver.name,
					cancellation, callerCanceled, driverFirst), func(t *testing.T) {
					t.Parallel()
					var cause error = &pgconn.PgError{Code: driver.code, Message: "private SQL", Detail: "private row"}
					if driver.code == "" {
						cause = knowledgeSanitizeConnectionFailure(t, io.EOF)
					}
					joined := errors.Join(denial, cancellation, cause)
					if driverFirst {
						joined = errors.Join(cause, cancellation, denial)
					}
					ctx, cancel := knowledgeSanitizeCallerContext(t.Context(), cancellation, callerCanceled)
					defer cancel()
					knowledgeSanitizeCheckFailure(ctx, t, fmt.Errorf("review: %w", joined), driver.want)
				})
			}
		}
	}
}

func TestSanitizeKnowledgeReadsConnectionOwnCancellation(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, callerCanceled := range []bool{false, true} {
			for _, driverFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/callerCanceled=%t/driverFirst=%t", cancellation,
					callerCanceled, driverFirst), func(t *testing.T) {
					t.Parallel()
					connection := knowledgeSanitizeConnectionFailure(t, cancellation)
					joined := errors.Join(denial, connection)
					if driverFirst {
						joined = errors.Join(connection, denial)
					}
					ctx, cancel := knowledgeSanitizeCallerContext(t.Context(), cancellation, callerCanceled)
					defer cancel()
					want := core.ErrDatabase
					if callerCanceled {
						want = cancellation
					}
					knowledgeSanitizeCheckFailure(ctx, t, fmt.Errorf("review: %w", joined), want)
				})
			}
		}
	}
}

func knowledgeSanitizeCheckFailure(ctx context.Context, t *testing.T, domainError, want error) {
	t.Helper()
	domain := &knowledgeSanitizeFailureDomain{proposalError: domainError}
	reader := KnowledgeReader{Domain: domain}
	original := knowledgeSanitizeFailureRead("first")
	value := &agent.KnowledgeContext{
		Reads: []agent.KnowledgeReadResult{original, knowledgeSanitizeFailureRead("second")}, Remaining: 0,
	}
	err := reader.SanitizeKnowledgeReads(ctx, "owner", value)
	require.Equal(t, want, err, "only a public SQL or caller-cancellation sentinel leaves the reader")
	require.ErrorIs(t, err, want)
	require.NotContains(t, err.Error(), "private")
	require.Equal(t, original, value.Reads[0])
	require.Equal(t, []string{"deletions", "first"}, domain.calls)
	require.Zero(t, value.Remaining)
}

func knowledgeSanitizeCallerContext(
	parent context.Context, cancellation error, canceled bool,
) (context.Context, context.CancelFunc) {
	if !canceled {
		return context.WithCancel(parent)
	}
	if errors.Is(cancellation, context.DeadlineExceeded) {
		return context.WithDeadline(parent, time.Time{})
	}
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx, cancel
}

// Use the concrete pgconn wrapper without network I/O or private diagnostic loss.
func knowledgeSanitizeConnectionFailure(t *testing.T, cause error) error {
	t.Helper()
	config, err := pgconn.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, cause
	}
	_, err = pgconn.ConnectConfig(t.Context(), config)
	var connection *pgconn.ConnectError
	require.ErrorAs(t, err, &connection)
	require.ErrorIs(t, err, cause)
	require.Contains(t, err.Error(), "private-user")
	return err
}

func TestSanitizeKnowledgeReadsAggregatedConnectionFailures(t *testing.T) {
	t.Parallel()
	denial := &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, callerCanceled := range []bool{false, true} {
			for _, shape := range []string{"mixed inside connection", "cancellation sibling first", "positive sibling first"} {
				t.Run(fmt.Sprintf("%s/callerCanceled=%t/%s", cancellation, callerCanceled, shape), func(t *testing.T) {
					t.Parallel()
					var joined error
					if shape == "mixed inside connection" {
						joined = errors.Join(
							denial,
							knowledgeSanitizeConnectionFailure(t, errors.Join(io.EOF, cancellation)),
						)
					} else {
						positive := knowledgeSanitizeConnectionFailure(t, io.EOF)
						canceled := knowledgeSanitizeConnectionFailure(t, cancellation)
						if shape == "cancellation sibling first" {
							joined = errors.Join(denial, canceled, positive)
						} else {
							joined = errors.Join(positive, canceled, denial)
						}
					}
					ctx, cancel := knowledgeSanitizeCallerContext(t.Context(), cancellation, callerCanceled)
					defer cancel()
					knowledgeSanitizeCheckFailure(ctx, t, fmt.Errorf("review: %w", joined), core.ErrDatabase)
				})
			}
		}
	}
}
