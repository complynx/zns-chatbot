package readsource

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

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
	locks    int
}

func (tx *proposalWindowTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	tx.locks++
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
	_, err := lockProposalValidity(t.Context(), tx, "alice", refs)
	require.ErrorIs(t, err, tx.lockErr, "bounded proposal closures may form a larger validation-only union")
	require.Equal(t, 1, tx.locks)
}
