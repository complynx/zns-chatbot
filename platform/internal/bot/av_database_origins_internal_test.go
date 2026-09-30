package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

const (
	r37Private       = "private-r37-detail"
	r37LockQuery     = "SELECT pg_advisory_lock"
	r37UnlockQuery   = "SELECT pg_advisory_unlock"
	r37AttachQuery   = "SELECT attachment_id FROM bot.media_intake"
	r37CachedQuery   = "SELECT status,private_result FROM bot.av_results"
	r37RoundsInsert  = "INSERT INTO bot.av_refinements"
	r37MetadataQuery = "SELECT m.av_kind,m.attachment_id,a.duration_num"
)

const r37ValidResult = `{"status":"ready","duration":{"numerator":3,"denominator":1},` +
	`"transcript":{"status":"done","text":"hello"}}`

// Valid JSON whose duration shape is incompatible with mediaproc.Rational.
const r37IncompatibleResult = `{"status":"ready","duration":"long-private-shape"}`

func r37Incoming() incoming { return incoming{owner: "alice", mediaID: "m", chat: 1} }

// r37OK answers an Exec with no rows; for QueryRow it is an empty result.
func r37OK() pgReply { return pgReply{action: pgRows} }

func r37Rows(oids []uint32, row ...[]byte) pgReply {
	return pgReply{action: pgRows, oids: oids, rows: [][][]byte{row}}
}

func r37NoRows(oids ...uint32) pgReply { return pgReply{action: pgRows, oids: oids} }

func r37Text(value string) pgReply { return r37Rows([]uint32{pgtype.TextOID}, []byte(value)) }

func r37Unlocked() pgReply { return r37Rows([]uint32{pgtype.BoolOID}, []byte("t")) }

func r37Cached(status string, private []byte) pgReply {
	return r37Rows([]uint32{pgtype.TextOID, pgtype.JSONBOID}, []byte(status), private)
}

func r37Metadata() pgReply {
	return r37Rows(
		[]uint32{pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.Int8OID},
		[]byte("video"), []byte("a"), []byte("10"), []byte("1"),
	)
}

func r37Seen(queries <-chan string) []string {
	var seen []string
	for {
		select {
		case query := <-queries:
			seen = append(seen, query)
		default:
			return seen
		}
	}
}

func r37RequireLast(t *testing.T, queries <-chan string, prefix string) []string {
	t.Helper()
	seen := r37Seen(queries)
	require.NotEmpty(t, seen)
	require.True(t, strings.HasPrefix(seen[len(seen)-1], prefix), "last query %q, want %q", seen[len(seen)-1], prefix)
	return seen
}

func r37RequireSafe(t *testing.T, err error) {
	t.Helper()
	requireSanitizedDatabaseError(t, err)
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), r37Private)
	var driver *pgconn.ConnectError
	require.NotErrorAs(t, err, &driver)
}

func r37RequireNotDatabase(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.NotErrorIs(t, err, core.ErrDatabase)
	require.False(t, core.IsDatabaseFailure(err))
}

// r37API serves one authorized attachment "a" and counts metadata authorizations.
func r37API(t *testing.T) (appclient.Client, *atomic.Int32) {
	t.Helper()
	metadata := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/media/a":
			metadata.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"a"}`))
		case "/v1/media/a/file":
			_, _ = w.Write([]byte("clip"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}, metadata
}

type r37AV struct {
	preprocess   func(context.Context) (mediaproc.Result, error)
	storyboard   func(context.Context) (mediaproc.Result, error)
	preprocessed atomic.Int32
	storyboards  atomic.Int32
}

func (a *r37AV) Preprocess(ctx context.Context, _ mediaclient.Kind, _ []byte) (mediaproc.Result, error) {
	a.preprocessed.Add(1)
	if a.preprocess == nil {
		return mediaproc.Result{Status: avReady, Duration: mediaproc.Rational{Numerator: 1, Denominator: 1}}, nil
	}
	return a.preprocess(ctx)
}

func (a *r37AV) Storyboard(
	ctx context.Context, _ mediaclient.Kind, _ []byte, _ mediaclient.Range,
) (mediaproc.Result, error) {
	a.storyboards.Add(1)
	if a.storyboard == nil {
		return mediaproc.Result{Status: avReady}, nil
	}
	return a.storyboard(ctx)
}

func r37Bot(t *testing.T, replies ...pgReply) (*Bot, <-chan string, *r37AV, *atomic.Int32) {
	t.Helper()
	b, queries := sqlBot(t, nil, replies...)
	api, metadata := r37API(t)
	av := &r37AV{}
	b.API = api
	b.AV = av
	return b, queries, av, metadata
}

// r37UnreachablePool fails at the real pgx dial with private diagnostics.
func r37UnreachablePool(t *testing.T) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	dials := &atomic.Int32{}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New(r37Private)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, dials
}

func r37Proposal() agent.MediaProposal {
	return agent.MediaProposal{MediaID: "m", StartMS: 0, EndMS: 1000, FrameCount: 2}
}

func r37Input() *agent.Input {
	return &agent.Input{AVInspection: &agent.AVInspectionContext{Remaining: 2}}
}

func TestR37AVSingleSQLSitesAreSanitized(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, prefix string
		run          func(*Bot, context.Context) error
	}{
		{"finish consumed voice", "UPDATE bot.media_intake SET status='done',last_action='answer'",
			func(b *Bot, ctx context.Context) error {
				return b.finishConsumedVoice(ctx, "alice", interaction.SavedPlan{MediaID: "m", AVIDs: []string{"m"}})
			}},
		{"purge expired AV", "UPDATE bot.av_results a SET private_result=NULL FROM bot.media_intake m",
			(*Bot).purgeExpiredAV},
		{"prepare kind", "SELECT av_kind FROM bot.media_intake", func(b *Bot, ctx context.Context) error {
			_, err := b.prepareAV(ctx, r37Incoming())
			return err
		}},
		{"current attachment AV input", "SELECT m.av_kind,a.private_result", func(b *Bot, ctx context.Context) error {
			_, err := b.addAVInput(ctx, "alice", "m", &agent.Input{})
			return err
		}},
		{"current AV kind", "SELECT av_kind FROM bot.media_intake", func(b *Bot, ctx context.Context) error {
			return b.addCurrentAV(ctx, r37Incoming(), &agent.Input{})
		}},
		{"inspection rounds", "SELECT rounds FROM bot.av_refinements", func(b *Bot, ctx context.Context) error {
			inspection, err := b.avInspectionContext(ctx, "alice", 1)
			if inspection != nil {
				return errors.New("unexpected inspection context")
			}
			return err
		}},
		{"refine metadata", r37MetadataQuery, func(b *Bot, ctx context.Context) error {
			notice, err := b.refineAV(ctx, "alice", 1, r37Proposal(), r37Input())
			if notice != "" {
				return errors.New("unexpected notice")
			}
			return err
		}},
		{"reserve round", r37RoundsInsert, func(b *Bot, ctx context.Context) error {
			_, err := b.reserveAVRound(ctx, "alice", 1)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries, av, metadata := r37Bot(t, pgReply{action: pgDrop})
			r37RequireSafe(t, test.run(b, t.Context()))
			seen := r37RequireLast(t, queries, test.prefix)
			require.Len(t, seen, 1, "the fault is the first and only SQL statement")
			require.Zero(t, metadata.Load())
			require.Zero(t, av.preprocessed.Load())
			require.Zero(t, av.storyboards.Load())
		})
	}
}

func TestR37InitialAVAcquireFailureIsSanitized(t *testing.T) {
	t.Parallel()
	pool, dials := r37UnreachablePool(t)
	api, metadata := r37API(t)
	av := &r37AV{}
	b := &Bot{DB: pool, API: api, AV: av}
	_, err := b.initialAV(t.Context(), r37Incoming(), "video")
	r37RequireSafe(t, err)
	require.Positive(t, dials.Load(), "the fault must come from connection acquisition")
	require.Zero(t, metadata.Load())
	require.Zero(t, av.preprocessed.Load())
}

func TestR37InitialAVSequentialSQLFailuresAreSanitized(t *testing.T) {
	t.Parallel()
	cachedMissing := r37NoRows(pgtype.TextOID, pgtype.JSONBOID)
	for _, test := range []struct {
		name         string
		replies      []pgReply
		prefix       string
		metadata     int32
		preprocessed int32
	}{
		{"advisory lock", []pgReply{{action: pgDrop}}, r37LockQuery, 0, 0},
		{"active attachment", []pgReply{r37OK(), {action: pgDrop}}, r37AttachQuery, 0, 0},
		{"cached result", []pgReply{r37OK(), r37Text("a"), {action: pgDrop}}, r37CachedQuery, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries, av, metadata := r37Bot(t, test.replies...)
			_, err := b.initialAV(t.Context(), r37Incoming(), "video")
			r37RequireSafe(t, err)
			seen := r37RequireLast(t, queries, test.prefix)
			require.Len(t, seen, len(test.replies), "the dropped statement is the intended one")
			require.Equal(t, test.metadata, metadata.Load())
			require.Equal(t, test.preprocessed, av.preprocessed.Load())
			require.Zero(t, b.DB.Stat().TotalConns(), "a faulted locked connection is never returned to the pool")
		})
	}
	t.Run("persisting a successful preprocess", func(t *testing.T) {
		t.Parallel()
		b, queries, av, metadata := r37Bot(t, r37OK(), r37Text("a"), cachedMissing, pgReply{action: pgDrop})
		conn, err := b.DB.Acquire(t.Context())
		require.NoError(t, err)
		// The wire fixture uses simple protocol, which needs an explicit type for
		// a struct parameter. Production learns JSONB from the prepared statement.
		conn.Conn().TypeMap().RegisterDefaultPgType(mediaproc.Result{}, "jsonb")
		conn.Release()
		result, err := b.initialAV(t.Context(), r37Incoming(), "video")
		r37RequireSafe(t, err)
		require.Equal(t, avReady, result.Status, "the processed result is not replaced by provider policy")
		require.Equal(t, int32(1), av.preprocessed.Load())
		require.Equal(t, int32(2), metadata.Load())
		seen := r37RequireLast(t, queries, "INSERT INTO bot.av_results")
		require.Len(t, seen, 4, "the fault must reach the INSERT on the wire")
		require.Zero(t, b.DB.Stat().TotalConns(), "a faulted locked connection is never returned to the pool")
	})
}

func TestR37InitialAVCachedResultJSON(t *testing.T) {
	t.Parallel()
	var valid mediaproc.Result
	require.NoError(t, json.Unmarshal([]byte(r37ValidResult), &valid))
	for _, test := range []struct {
		name    string
		status  string
		private []byte
		want    mediaproc.Result
	}{
		{"valid cached JSON", "partial", []byte(r37ValidResult), valid},
		{"SQL NULL uses stored status", "partial", nil, mediaproc.Result{Status: "partial"}},
		{"JSON null uses stored status", "failed", []byte("null"), mediaproc.Result{Status: "failed"}},
		{"JSON zero object stays saved", "failed", []byte("{}"), mediaproc.Result{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries, av, metadata := r37Bot(t, r37OK(), r37Text("a"), r37Cached(test.status, test.private),
				r37Unlocked())
			result, err := b.initialAV(t.Context(), r37Incoming(), "video")
			require.NoError(t, err)
			require.Equal(t, test.want, result)
			require.Zero(t, av.preprocessed.Load(), "a cached row never reaches the processor")
			require.Equal(t, int32(1), metadata.Load())
			r37RequireLast(t, queries, r37UnlockQuery)
			require.Equal(t, int32(1), b.DB.Stat().IdleConns(), "an unlocked connection is released")
		})
	}
	t.Run("incompatible JSON is an ordinary decode error", func(t *testing.T) {
		t.Parallel()
		b, queries, av, _ := r37Bot(t, r37OK(), r37Text("a"),
			r37Cached(avReady, []byte(r37IncompatibleResult)), r37Unlocked())
		_, err := b.initialAV(t.Context(), r37Incoming(), "video")
		r37RequireNotDatabase(t, err)
		var incompatible *json.UnmarshalTypeError
		require.ErrorAs(t, err, &incompatible)
		require.NotContains(t, err.Error(), "long-private-shape")
		require.NotContains(t, err.Error(), r37Private)
		require.Zero(t, av.preprocessed.Load())
		r37RequireLast(t, queries, r37UnlockQuery)
		require.Equal(t, int32(1), b.DB.Stat().IdleConns())
	})
}

func TestR37InitialAVExpectedOutcomes(t *testing.T) {
	t.Parallel()
	t.Run("inactive intake keeps NoRows", func(t *testing.T) {
		t.Parallel()
		b, queries, _, metadata := r37Bot(t, r37OK(), r37NoRows(pgtype.TextOID), r37Unlocked())
		_, err := b.initialAV(t.Context(), r37Incoming(), "video")
		require.ErrorIs(t, err, pgx.ErrNoRows)
		r37RequireNotDatabase(t, err)
		require.Zero(t, metadata.Load())
		r37RequireLast(t, queries, r37UnlockQuery)
		require.Equal(t, int32(1), b.DB.Stat().IdleConns())
	})
	t.Run("canceled before acquisition", func(t *testing.T) {
		t.Parallel()
		b, queries, _, _ := r37Bot(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := b.initialAV(ctx, r37Incoming(), "video")
		require.ErrorIs(t, err, context.Canceled)
		r37RequireNotDatabase(t, err)
		require.Empty(t, r37Seen(queries))
	})
	t.Run("canceled preprocess", func(t *testing.T) {
		t.Parallel()
		b, queries, av, metadata := r37Bot(t, r37OK(), r37Text("a"), r37NoRows(pgtype.TextOID, pgtype.JSONBOID),
			r37Unlocked())
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		av.preprocess = func(context.Context) (mediaproc.Result, error) {
			cancel()
			return mediaproc.Result{}, errors.New("provider interrupted")
		}
		_, err := b.initialAV(ctx, r37Incoming(), "video")
		require.ErrorIs(t, err, context.Canceled)
		r37RequireNotDatabase(t, err)
		require.Equal(t, int32(1), metadata.Load(), "no re-authorization or write after cancellation")
		r37RequireLast(t, queries, r37UnlockQuery)
		require.Equal(t, int32(1), b.DB.Stat().IdleConns(), "cleanup still unlocks and releases")
	})
}

func TestR37PrepareAVTerminalUpdateFailure(t *testing.T) {
	t.Parallel()
	rejected := []byte(`{"status":"rejected","reason":"unsupported","duration":{"numerator":0,"denominator":1}}`)
	b, queries, av, metadata := r37Bot(t, r37Text("video"), r37OK(), r37Text("a"), r37Cached("rejected", rejected),
		r37Unlocked(), pgReply{action: pgDrop})
	handled, err := b.prepareAV(t.Context(), r37Incoming())
	r37RequireSafe(t, err)
	require.True(t, handled)
	seen := r37RequireLast(t, queries, "UPDATE bot.media_intake SET status='done',notice")
	require.Len(t, seen, 6)
	require.True(t, strings.HasPrefix(seen[4], r37UnlockQuery), "the lock is released before the terminal write")
	require.Equal(t, int32(1), metadata.Load())
	require.Zero(t, av.preprocessed.Load())
}

func TestR37PrepareAVKindControls(t *testing.T) {
	t.Parallel()
	b, _, _, _ := r37Bot(t, r37NoRows(pgtype.TextOID))
	handled, err := b.prepareAV(t.Context(), r37Incoming())
	require.ErrorIs(t, err, pgx.ErrNoRows)
	r37RequireNotDatabase(t, err)
	require.False(t, handled)
	b, queries, _, _ := r37Bot(t, r37Text(""))
	handled, err = b.prepareAV(t.Context(), r37Incoming())
	require.NoError(t, err)
	require.False(t, handled)
	require.Len(t, r37Seen(queries), 1, "non-AV media stops after the kind lookup")
}

func TestR37AddAVInputStoredResult(t *testing.T) {
	t.Parallel()
	row := func(kind string, private []byte) pgReply {
		return r37Rows([]uint32{pgtype.TextOID, pgtype.JSONBOID}, []byte(kind), private)
	}
	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		b, _, _, _ := r37Bot(t, row("voice", []byte(r37ValidResult)))
		var input agent.Input
		handled, err := b.addAVInput(t.Context(), "alice", "m", &input)
		require.NoError(t, err)
		require.True(t, handled)
		require.NotNil(t, input.AV)
		require.Equal(t, "hello", input.AV.Transcript.Text)
		require.Equal(t, mediaproc.Rational{Numerator: 3, Denominator: 1}, input.AV.Duration)
	})
	for name, private := range map[string][]byte{"SQL NULL": nil, "JSON null": []byte("null")} {
		t.Run(name+" is unavailable", func(t *testing.T) {
			t.Parallel()
			b, _, _, _ := r37Bot(t, row("voice", private))
			var input agent.Input
			handled, err := b.addAVInput(t.Context(), "alice", "m", &input)
			require.True(t, handled)
			require.EqualError(t, err, "private AV result unavailable")
			r37RequireNotDatabase(t, err)
			require.Nil(t, input.AV)
		})
	}
	t.Run("incompatible JSON", func(t *testing.T) {
		t.Parallel()
		b, _, _, _ := r37Bot(t, row("voice", []byte(r37IncompatibleResult)))
		var input agent.Input
		handled, err := b.addAVInput(t.Context(), "alice", "m", &input)
		require.False(t, handled)
		r37RequireNotDatabase(t, err)
		var incompatible *json.UnmarshalTypeError
		require.ErrorAs(t, err, &incompatible)
		require.NotContains(t, err.Error(), "long-private-shape")
		require.NotContains(t, err.Error(), r37Private)
		require.Nil(t, input.AV)
	})
	t.Run("missing intake keeps NoRows", func(t *testing.T) {
		t.Parallel()
		b, _, _, _ := r37Bot(t, r37NoRows(pgtype.TextOID, pgtype.JSONBOID))
		handled, err := b.addAVInput(t.Context(), "alice", "m", &agent.Input{})
		require.False(t, handled)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		r37RequireNotDatabase(t, err)
	})
	t.Run("non-AV media", func(t *testing.T) {
		t.Parallel()
		b, _, _, _ := r37Bot(t, row("", nil))
		handled, err := b.addAVInput(t.Context(), "alice", "m", &agent.Input{})
		require.NoError(t, err)
		require.False(t, handled)
	})
}

func TestR37CurrentAVAndInspectionControls(t *testing.T) {
	t.Parallel()
	require.NoError(t, (&Bot{}).addCurrentAV(t.Context(), incoming{owner: "alice"}, &agent.Input{}))
	b, _, _, _ := r37Bot(t, r37NoRows(pgtype.TextOID))
	err := b.addCurrentAV(t.Context(), r37Incoming(), &agent.Input{})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	r37RequireNotDatabase(t, err)
	b, _, _, _ = r37Bot(t, r37Text(""))
	require.NoError(t, b.addCurrentAV(t.Context(), r37Incoming(), &agent.Input{}))

	b, _, _, _ = r37Bot(t, r37NoRows(pgtype.Int8OID))
	inspection, err := b.avInspectionContext(t.Context(), "alice", 1)
	require.NoError(t, err)
	require.Equal(t, 2, inspection.Remaining, "no prior rounds keeps the full budget")
	b, _, _, _ = r37Bot(t, r37Rows([]uint32{pgtype.Int8OID}, []byte("1")))
	inspection, err = b.avInspectionContext(t.Context(), "alice", 1)
	require.NoError(t, err)
	require.Equal(t, 1, inspection.Remaining)
}

func TestR37InspectionCancellationIsNotDatabase(t *testing.T) {
	t.Parallel()
	b, queries, _, _ := r37Bot(t, pgReply{action: pgHang})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-queries:
		case <-time.After(5 * time.Second):
		}
		cancel()
	}()
	inspection, err := b.avInspectionContext(ctx, "alice", 1)
	require.Nil(t, inspection)
	require.ErrorIs(t, err, context.Canceled)
	r37RequireNotDatabase(t, err)
}

func TestR37RefineAVRoundBoundaries(t *testing.T) {
	t.Parallel()
	t.Run("reserve failure never reaches the provider", func(t *testing.T) {
		t.Parallel()
		b, queries, av, metadata := r37Bot(t, r37Metadata(), pgReply{action: pgDrop})
		input := r37Input()
		notice, err := b.refineAV(t.Context(), "alice", 1, r37Proposal(), input)
		r37RequireSafe(t, err)
		require.Empty(t, notice)
		seen := r37RequireLast(t, queries, r37RoundsInsert)
		require.Len(t, seen, 2)
		require.Equal(t, int32(1), metadata.Load())
		require.Zero(t, av.storyboards.Load())
		require.Equal(t, 2, input.AVInspection.Remaining)
	})
	t.Run("exhausted budget", func(t *testing.T) {
		t.Parallel()
		b, _, av, _ := r37Bot(t, r37Metadata(), r37NoRows(pgtype.Int8OID))
		input := r37Input()
		notice, err := b.refineAV(t.Context(), "alice", 1, r37Proposal(), input)
		require.NoError(t, err)
		require.Equal(t, i18n.AVInspectLimit, notice)
		require.Zero(t, av.storyboards.Load())
		require.Equal(t, 2, input.AVInspection.Remaining)
	})
	t.Run("missing media", func(t *testing.T) {
		t.Parallel()
		b, _, av, metadata := r37Bot(t, r37NoRows(pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.Int8OID))
		notice, err := b.refineAV(t.Context(), "alice", 1, r37Proposal(), r37Input())
		require.NoError(t, err)
		require.Equal(t, i18n.MediaUnavailable, notice)
		require.Zero(t, metadata.Load())
		require.Zero(t, av.storyboards.Load())
	})
	t.Run("provider failure keeps AVFailed after reserving", func(t *testing.T) {
		t.Parallel()
		b, _, av, _ := r37Bot(t, r37Metadata(), r37Rows([]uint32{pgtype.Int8OID}, []byte("2")))
		av.storyboard = func(context.Context) (mediaproc.Result, error) { return mediaproc.Result{}, io.EOF }
		input := r37Input()
		notice, err := b.refineAV(t.Context(), "alice", 1, r37Proposal(), input)
		require.NoError(t, err)
		require.Equal(t, i18n.AVFailed, notice)
		require.Equal(t, int32(1), av.storyboards.Load())
		require.Zero(t, input.AVInspection.Remaining, "the reserved second round is accounted")
	})
	t.Run("provider cancellation", func(t *testing.T) {
		t.Parallel()
		b, _, av, _ := r37Bot(t, r37Metadata(), r37Rows([]uint32{pgtype.Int8OID}, []byte("1")))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		av.storyboard = func(context.Context) (mediaproc.Result, error) {
			cancel()
			return mediaproc.Result{}, errors.New("provider interrupted")
		}
		notice, err := b.refineAV(ctx, "alice", 1, r37Proposal(), r37Input())
		require.Empty(t, notice)
		require.ErrorIs(t, err, context.Canceled)
		r37RequireNotDatabase(t, err)
	})
}
