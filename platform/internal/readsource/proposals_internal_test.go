package readsource

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

func TestProposalClosuresValidateHistoryEntriesSeparately(t *testing.T) {
	t.Parallel()
	for _, distinct := range []bool{false, true} {
		t.Run(strconv.FormatBool(distinct), func(t *testing.T) {
			t.Parallel()
			refs := make([]Authority, MaxAuthorities+1)
			for i := range refs {
				scope := "shared"
				if distinct {
					scope = strconv.Itoa(i)
				}
				generation := int64(0)
				refs[i] = Authority{
					Causal: &CausalSource{
						Actor:      "alice",
						Generation: &generation,
						Authorities: []Authority{
							{
								Knowledge: knowledgeauthority.ReadAuthority{
									Kind:  knowledgeauthority.Review,
									Scope: scope,
								},
							},
						},
					},
				}
			}
			before := CloneAuthorities(refs)
			closures, err := proposalClosures(t.Context(), nil, refs)
			require.NoError(t, err)
			require.Len(t, closures, len(refs))
			for i := range refs {
				require.Equal(t, refs[i], closures[i][0])
			}
			closures[0][0].Causal.Authorities[0].Knowledge.Scope = "changed"
			*closures[0][0].Causal.Generation = 99
			require.Equal(t, before, refs, "internal expansion must not alias input records")
		})
	}
}

func TestProposalClosuresKeepStoredAuthorityLimits(t *testing.T) {
	t.Parallel()
	generation := int64(0)
	leaves := make([]Authority, MaxAuthorities)
	for i := range leaves {
		leaves[i] = Authority{
			Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: strconv.Itoa(i)},
		}
	}
	oversized := []Authority{{Causal: &CausalSource{Actor: "alice", Generation: &generation, Authorities: leaves}}}
	_, err := proposalClosures(t.Context(), nil, oversized)
	require.ErrorIs(t, err, ErrLimit)
	_, err = Capture("alice", Derivation{Generation: &generation, Authorities: leaves})
	require.ErrorIs(t, err, ErrLimit)
	_, err = Merge(
		leaves,
		[]Authority{{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "extra"}}},
	)
	require.ErrorIs(t, err, ErrLimit)
}

type proposalWindowTx struct {
	pgx.Tx

	t        *testing.T
	evidence map[int64][]byte
	lockErr  error
	queries  []string
	args     [][]any
}

// Query records every lock query and fails it with a private driver-like error.
// Known SQL operations sanitize that error, so observation uses the recording.
func (tx *proposalWindowTx) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	return nil, tx.lockErr
}
func (tx *proposalWindowTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	require.Contains(tx.t, query, "core.knowledge_proposals")
	id, ok := args[0].(int64)
	require.True(tx.t, ok)
	raw, ok := tx.evidence[id]
	require.True(tx.t, ok)
	return proposalWindowRow{t: tx.t, raw: raw}
}

type proposalWindowRow struct {
	t   *testing.T
	raw []byte
}

func (row proposalWindowRow) Scan(dest ...any) error {
	raw, ok := dest[0].(*[]byte)
	require.True(row.t, ok)
	submitted, ok := dest[1].(*bool)
	require.True(row.t, ok)
	*raw = append([]byte{}, row.raw...)
	*submitted = false
	return nil
}

func TestProposalWindowUnionDoesNotUseStoredLimit(t *testing.T) {
	t.Parallel()
	tx := &proposalWindowTx{
		t:        t,
		evidence: map[int64][]byte{},
		lockErr:  errors.New("reached globally ordered lock pass"),
	}
	refs := []Authority{}
	for _, id := range []int64{1000, 2000} {
		leaves := make([]Authority, MaxAuthorities/2)
		for i := range leaves {
			leaves[i] = Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:  knowledgeauthority.Review,
					Scope: strconv.FormatInt(id, 10) + "-" + strconv.Itoa(i),
				},
			}
		}
		generation := int64(0)
		evidence := []Authority{{Causal: &CausalSource{Actor: "alice", Generation: &generation, Authorities: leaves}}}
		require.True(t, Valid(evidence))
		raw, err := json.Marshal(evidence)
		require.NoError(t, err)
		tx.evidence[id] = raw
		refs = append(
			refs,
			Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:       knowledgeauthority.DerivedProposal,
					Owner:      "alice",
					ProposalID: id,
				},
			},
		)
	}
	// Each stored closure fits the persisted bound; their validation union does
	// not, so an aggregate Merge would reject this window before any lock.
	closures, err := proposalClosures(t.Context(), tx, refs)
	require.NoError(t, err)
	require.Len(t, closures, len(refs))
	for _, closure := range closures {
		require.True(t, Valid(closure))
	}
	require.False(t, Valid(proposalWindowAuthorities(closures)), "the window union must exceed the stored bound")
	_, err = Merge(closures...)
	require.ErrorIs(t, err, ErrLimit)
	require.Empty(t, tx.queries, "closure expansion takes no locks")

	_, err = lockProposalValidity(t.Context(), tx, "alice", refs)
	// The larger union reaches exactly one globally ordered event-lock query.
	// This fixture has no registration leaves, so its event list is empty.
	if assert.Len(t, tx.queries, 1, "bounded proposal closures may form a larger validation-only union: %v", err) {
		assert.Contains(t, tx.queries[0], "FROM core.pass_events WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE")
		assert.Equal(t, []any{[]string{}}, tx.args[0])
	}
	// The known SQL operation sanitizes the injected failure.
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, core.IsDatabaseFailure(err))
	require.NotErrorIs(t, err, core.ErrDatabaseSerialization)
	require.NotErrorIs(t, err, ErrLimit, "the window must not apply the stored-record budget")
	require.NotErrorIs(t, err, tx.lockErr, "driver diagnostics must not escape")
	require.NotContains(t, err.Error(), tx.lockErr.Error())
}
