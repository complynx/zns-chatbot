package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestMediaIntakeUploadSQLOrigin(t *testing.T) {
	t.Parallel()
	b, queries := sqlBot(t, nil, pgReply{action: pgDrop})
	var uploaded atomic.Int32
	b.API = mediaRenderAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/media" {
			http.NotFound(w, r)
			return
		}
		uploaded.Add(1)
		mediaRenderReply(w, http.StatusOK, `{"id":"attachment"}`)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botsecret/getFile":
			mediaRenderReply(w, http.StatusOK, `{"ok":true,"result":{"file_path":"receipt.pdf"}}`)
		case "/file/botsecret/receipt.pdf":
			_, _ = w.Write([]byte("receipt"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	b.TG = telegram.Client{Base: server.URL, Token: "secret"}
	err := b.saveMediaUpload(t.Context(), incoming{owner: "alice", mediaID: "m"}, telegram.Update{
		ID: 1, Message: &telegram.Message{Document: &telegram.Document{FileID: "opaque", Filename: "receipt.pdf"}},
	})
	requireSafeDatabaseFailure(t, err)
	require.Equal(t, int32(1), uploaded.Load(), "the actual upload must precede the failed insert")
	r37RequireLast(t, queries, "INSERT INTO bot.media_intake")
}

func TestMediaContextVisibleHintSQLAndJSON(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		reply    pgReply
		wantSQL  bool
		wantJSON bool
		absent   bool
		wantID   string
	}{
		{name: "transport EOF", reply: pgReply{action: pgDrop}, wantSQL: true},
		{name: "no row", reply: r37NoRows(pgtype.JSONBOID), absent: true},
		{name: "SQL null", reply: r37Rows([]uint32{pgtype.JSONBOID}, nil), wantID: "m"},
		{name: "JSON null", reply: r37Rows([]uint32{pgtype.JSONBOID}, []byte(`null`)), wantID: "m"},
		{name: "empty object", reply: r37Rows([]uint32{pgtype.JSONBOID}, []byte(`{}`))},
		{name: "incompatible JSON", reply: r37Rows([]uint32{pgtype.JSONBOID}, []byte(`[]`)), wantJSON: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries := sqlBot(t, nil, test.reply,
				r37NoRows(pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.Int8OID))
			hint, err := b.visibleMediaHint(t.Context(), "alice", "m", "choose")
			switch {
			case test.wantSQL:
				requireSafeDatabaseFailure(t, err)
			case test.wantJSON:
				var invalid *json.UnmarshalTypeError
				require.ErrorAs(t, err, &invalid)
				require.False(t, core.IsDatabaseFailure(err))
			case test.absent:
				require.ErrorIs(t, err, pgx.ErrNoRows)
				require.False(t, core.IsDatabaseFailure(err))
			default:
				require.NoError(t, err)
				require.Equal(t, test.wantID, hint.ID)
				if test.wantID != "" {
					require.Equal(t, "choose", hint.Kind)
				}
			}
			seen := r37Seen(queries)
			require.NotEmpty(t, seen)
			require.True(t, strings.HasPrefix(seen[0], "SELECT rendered FROM bot.media_intake"))
			if test.wantSQL || test.wantJSON || test.absent {
				require.Len(t, seen, 1, "failure must not continue to AV lookup")
			} else {
				require.Len(t, seen, 2)
			}
		})
	}
}

func TestMediaContextRecentSQLOrigins(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		reply pgReply
	}{
		{"transport EOF", pgReply{action: pgDrop}},
		{"scalar scan failure", r37Rows([]uint32{
			pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID,
			pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.TextOID,
		}, []byte("e"), []byte("m"), []byte("choose"), []byte(""), []byte(""), []byte(""), nil, []byte(""))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries := sqlBot(t, nil, test.reply)
			_, err := b.mediaRecent(t.Context(), "alice")
			requireSafeDatabaseFailure(t, err)
			r37RequireLast(t, queries, "SELECT COALESCE(registration_command")
		})
	}
}

func TestMediaIntakeProviderFailureIsNotSQL(t *testing.T) {
	t.Parallel()
	db, dials := faultedStartupPool(t)
	server := startupMenuServer(t, `{"ok":false,"error_code":429,"description":"retry","parameters":{"retry_after":7}}`)
	b := &Bot{DB: db, TG: telegram.Client{Base: server.URL, Token: "secret"}}
	err := b.saveMediaUpload(t.Context(), incoming{owner: "alice"}, telegram.Update{
		ID: 1, Message: &telegram.Message{Document: &telegram.Document{FileID: "opaque"}},
	})
	var provider *telegram.APIError
	require.ErrorAs(t, err, &provider)
	require.Equal(t, http.StatusTooManyRequests, provider.Code)
	require.False(t, core.IsDatabaseFailure(err))
	require.False(t, terminalMediaUploadFailure(err))
	require.Zero(t, dials.Load(), "provider failure must not reach the SQL write")
}

func TestMediaIntakePendingReadSQLOrigins(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		reply pgReply
	}{
		{"transport EOF", pgReply{action: pgDrop}},
		{"scalar scan failure", pgReply{action: pgRows,
			oids: []uint32{pgtype.TextOID, pgtype.TextOID}, rows: [][][]byte{{nil, []byte("choose")}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries := sqlBot(t, nil, test.reply)
			err := b.addMediaContext(t.Context(), incoming{owner: "alice"}, &agent.Input{})
			requireSafeDatabaseFailure(t, err)
			r37RequireLast(t, queries, "SELECT id,status FROM bot.media_intake")
		})
	}
}

// Reach the final selected-command read with valid preceding rows and API replies.
func TestMediaIntakeSelectedCommandSQLAndJSON(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		reply    pgReply
		wantSQL  bool
		wantJSON bool
	}{
		{name: "transport EOF", reply: pgReply{action: pgDrop}, wantSQL: true},
		{name: "no row", reply: r37NoRows(pgtype.JSONBOID)},
		{name: "SQL null", reply: r37Rows([]uint32{pgtype.JSONBOID}, nil)},
		{name: "JSON null", reply: r37Rows([]uint32{pgtype.JSONBOID}, []byte(`null`))},
		{name: "empty object", reply: r37Rows([]uint32{pgtype.JSONBOID}, []byte(`{}`))},
		{name: "incompatible JSON", wantJSON: true,
			reply: r37Rows([]uint32{pgtype.JSONBOID}, []byte(`{"version":"private-shape"}`))},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, queries := sqlBot(t, nil,
				r37Rows([]uint32{pgtype.TextOID, pgtype.TextOID}, []byte("m"), []byte("choose")),
				r37Rows([]uint32{pgtype.JSONBOID}, []byte(`null`)),
				r37NoRows(pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.Int8OID),
				r37NoRows(pgtype.TextOID, pgtype.TextOID, pgtype.TextOID, pgtype.TextOID,
					pgtype.TextOID, pgtype.TextOID, pgtype.Int8OID, pgtype.TextOID),
				r37NoRows(pgtype.JSONBOID), test.reply,
			)
			b.API = mediaRenderAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/orders") {
					mediaRenderReply(w, http.StatusOK, `{}`)
					return
				}
				if r.URL.Path == "/v1/passes/events" {
					mediaRenderReply(w, http.StatusOK, `[]`)
					return
				}
				http.NotFound(w, r)
			})
			err := b.addMediaContext(t.Context(), incoming{owner: "alice"}, &agent.Input{})
			switch {
			case test.wantSQL:
				requireSafeDatabaseFailure(t, err)
			case test.wantJSON:
				var invalid *json.UnmarshalTypeError
				require.ErrorAs(t, err, &invalid)
				require.False(t, core.IsDatabaseFailure(err))
			default:
				require.NoError(t, err)
			}
			seen := r37RequireLast(t, queries, "SELECT command FROM bot.proof_pending")
			require.Len(t, seen, 6, "the failure/control must reach the final read after all preceding reads")
		})
	}
}
