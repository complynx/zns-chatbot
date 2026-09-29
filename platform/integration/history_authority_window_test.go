package integration_test

import (
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func prepareAuthorityWindowScopes(t *testing.T, db *pgxpool.Pool, count int) {
	t.Helper()
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at)
 SELECT 'window-'||n,now()+interval '1 day' FROM generate_series(0,$1::integer-1) n`, count)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.knowledge_scopes(scope,event_id)
 SELECT id,id FROM core.pass_events WHERE id LIKE 'window-%' ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.knowledge_permissions(scope,actor,permission)
 SELECT 'window-'||n,'alice','review' FROM generate_series(0,$1::integer-1) n
 UNION ALL SELECT 'window-'||n,'bob','review' FROM generate_series(0,$1::integer-1) n WHERE n%2=0`, count)
	require.NoError(t, err)
}

func TestHistoryAuthorityWindowBeyondStoredLimits(t *testing.T) {
	t.Parallel()
	for _, distinct := range []bool{false, true} {
		t.Run(strconv.FormatBool(distinct), func(t *testing.T) {
			t.Parallel()
			db, _ := bookingFixture(t)
			const perEvent = 16
			count := perEvent
			if distinct {
				count *= conversation.MaxPage
			}
			prepareAuthorityWindowScopes(t, db, count)
			history := conversation.Service{DB: db}
			appendAuthorityWindowRecords(t, history, perEvent, distinct)
			for range 2 {
				window, err := history.Window(t.Context(), "alice", conversation.MaxPage)
				require.NoError(t, err)
				require.Len(t, window.Recent, conversation.MaxPage)
				require.Zero(t, window.Generation)
				for i, event := range window.Recent {
					require.Equal(t, "body "+strconv.Itoa(i), event.Text)
					require.False(t, event.Omitted)
					require.Len(t, event.ReadAuthorities, perEvent)
				}
				page, err := history.Read(t.Context(), "alice", conversation.Query{Limit: conversation.MaxPage})
				require.NoError(t, err)
				require.Len(t, page.Events, conversation.MaxPage)
				require.Zero(t, page.Generation)
			}
		})
	}
}

func TestAuthorityWindowKeepsPositionalOriginAndReader(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	count := readsource.MaxAuthorities + 1
	prepareAuthorityWindowScopes(t, db, count)
	refs := make([]readsource.Authority, 0, count+3)
	for i := range count {
		generation := int64(0)
		refs = append(refs, readsource.Authority{
			Causal: &readsource.CausalSource{
				Actor:      "alice",
				Generation: &generation,
				Authorities: []readsource.Authority{
					{
						Knowledge: knowledgeauthority.ReadAuthority{
							Kind:  knowledgeauthority.Review,
							Scope: "window-" + strconv.Itoa(i),
						},
					},
				},
			},
		})
	}
	refs = append(refs, readsource.CloneAuthorities(refs[2:3])...)
	stale := readsource.CloneAuthorities(refs[2:3])[0]
	*stale.Causal.Generation = 1
	refs = append(refs, stale)
	private := readsource.CloneAuthorities(refs[2:3])[0]
	private.Causal.PrivateHistory = true
	refs = append(refs, private)
	before := readsource.CloneAuthorities(refs)
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	validity, err := readsource.LockValidity(t.Context(), tx, "bob", refs)
	require.NoError(t, err)
	require.Len(t, validity.Origin, len(refs))
	require.Len(t, validity.Reader, len(refs))
	for i := range count {
		require.True(t, validity.Origin[i])
		require.Equal(t, i%2 == 0, validity.Reader[i], "input position %d", i)
	}
	require.True(t, validity.Origin[count])
	require.True(t, validity.Reader[count])
	require.False(t, validity.Origin[count+1])
	require.False(t, validity.Reader[count+1])
	require.True(t, validity.Origin[count+2])
	require.False(t, validity.Reader[count+2])
	require.Equal(t, before, refs)
	require.NoError(t, tx.Commit(t.Context()))
}

func appendAuthorityWindowRecords(t *testing.T, history conversation.Service, perEvent int, distinct bool) {
	t.Helper()
	for i := range conversation.MaxPage {
		refs := make([]readsource.Authority, perEvent)
		for j := range refs {
			index := j
			if distinct {
				index += i * perEvent
			}
			refs[j] = readsource.Authority{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:  knowledgeauthority.Review,
					Scope: "window-" + strconv.Itoa(index),
				},
			}
		}
		require.True(t, readsource.Valid(refs))
		require.NoError(
			t,
			history.AppendDerived(
				t.Context(),
				"alice",
				"window-record-"+strconv.Itoa(i),
				"body "+strconv.Itoa(i),
				0,
				refs,
			),
		)
	}
}
