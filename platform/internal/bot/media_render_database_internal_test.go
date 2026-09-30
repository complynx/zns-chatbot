package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// The connect hook observes the first SQL attempt; every attempt fails in transport.
func mediaRenderUnreachableDatabase(t *testing.T, connect func()) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		if connect != nil {
			connect()
		}
		return io.EOF
	}
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

func mediaRenderAPI(t *testing.T, handler http.HandlerFunc) appclient.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}
}

func mediaRenderReply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func mediaRenderFaultyPool(t *testing.T, db *pgxpool.Pool, prefix string) (*pgxpool.Pool, *receiptDatabaseFault) {
	t.Helper()
	fault := &receiptDatabaseFault{prefix: prefix}
	config := db.Config()
	config.ConnConfig.Tracer = fault
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	return broken, fault
}

type mediaRenderAV struct{}

func (mediaRenderAV) Preprocess(context.Context, mediaclient.Kind, []byte) (mediaproc.Result, error) {
	return mediaproc.Result{}, nil
}

func (mediaRenderAV) Storyboard(
	context.Context, mediaclient.Kind, []byte, mediaclient.Range,
) (mediaproc.Result, error) {
	return mediaproc.Result{}, nil
}

func TestMediaRenderDatabaseOrigins(t *testing.T) {
	t.Parallel()
	b := &Bot{DB: mediaRenderUnreachableDatabase(t, nil)}
	hint := agent.MediaHint{ID: "m"}
	require.ErrorIs(t, b.avHint(t.Context(), "alice", &hint), core.ErrDatabase)
	require.ErrorIs(t, b.clearAVResults(t.Context(), "alice", []string{"m"}), core.ErrDatabase)
	_, err := b.passMediaTextVisible(t.Context(), "alice", "m")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = b.mediaButton(t.Context(), "alice", "m", mediaOrderChoice, agent.MediaCandidate{OrderID: "o"}, "label")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = b.foodButton(t.Context(), "alice", "label", foodButtonCommand{MediaID: "m"})
	require.ErrorIs(t, err, core.ErrDatabase)
	in := incoming{owner: "alice", chat: 1}
	refusal := &core.ProblemError{Status: http.StatusConflict, Code: "stale_version"}
	err = b.mediaExecutionError(t.Context(), in, mediaIntake{ID: "m"}, refusal)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
}

func TestMediaRenderDatabaseExecutionErrorControls(t *testing.T) {
	t.Parallel()
	var connects atomic.Int32
	b := &Bot{DB: mediaRenderUnreachableDatabase(t, func() { connects.Add(1) })}
	in := incoming{owner: "alice", chat: 1}
	err := b.mediaExecutionError(t.Context(), in, mediaIntake{ID: "m"}, io.EOF)
	require.ErrorIs(t, err, io.EOF, "ordinary provider transport failure keeps its identity")
	require.False(t, core.IsDatabaseFailure(err))
	outage := &core.ProblemError{Status: http.StatusServiceUnavailable, Code: "provider_unavailable"}
	err = b.mediaExecutionError(t.Context(), in, mediaIntake{ID: "m"}, outage)
	require.ErrorIs(t, err, outage)
	require.False(t, core.IsDatabaseFailure(err))
	require.Zero(t, connects.Load(), "non-refusal errors never reach the terminal SQL write")
	// A refused command source is a domain refusal; only its terminal write is SQL.
	err = b.commitMediaReceipt(t.Context(), in, mediaIntake{ID: "m", CommandOrigin: originAgent,
		Command: &orders.Command{Name: stateProof, OrderID: "o"}})
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Positive(t, connects.Load())
}

func TestMediaRenderDatabaseResumedReceiptWrites(t *testing.T) {
	t.Parallel()
	type committer func(*Bot, context.Context, incoming, mediaIntake) error
	orderItem := func(proof string) mediaIntake {
		return mediaIntake{ID: "m", AttachmentID: "a", CommandOrigin: originManual, Command: &orders.Command{
			Name: stateProof, EventID: "e", OrderID: "o", Version: 1, Origin: originManual, Key: "m",
			ProofFile: proof,
		}}
	}
	registrationItem := func(proof string) mediaIntake {
		return mediaIntake{ID: "m", AttachmentID: "a", CommandOrigin: originManual,
			RegistrationCommand: &passbooking.Command{
				Name: stateProof, Event: "e", Version: 1, Key: "media-m", ProofID: proof,
			}}
	}
	foodItem := func(proof string) mediaIntake {
		return mediaIntake{ID: "m", AttachmentID: "a", CommandOrigin: originManual, FoodCommand: &legacyfood.Command{
			EventID: "e", OrderID: "o", Version: 1, Name: foodSubmitProof, Kind: legacyfood.Meals, Key: "m",
			ProofID: proof,
		}}
	}
	for _, test := range []struct {
		name   string
		item   mediaIntake
		commit committer
	}{
		{"order promoted command", orderItem(""), (*Bot).commitMediaReceipt},
		{"order done state", orderItem("proof-0"), (*Bot).commitMediaReceipt},
		{"registration promoted command", registrationItem(""), (*Bot).commitRegistrationReceipt},
		{"registration done state", registrationItem("proof-0"), (*Bot).commitRegistrationReceipt},
		{"food promoted command", foodItem(""), (*Bot).commitFoodReceipt},
		{"food done state", foodItem("proof-0"), (*Bot).commitFoodReceipt},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var requests, atConnect atomic.Int32
			atConnect.Store(-1)
			db := mediaRenderUnreachableDatabase(t, func() { atConnect.CompareAndSwap(-1, requests.Load()) })
			api := mediaRenderAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				mediaRenderReply(w, http.StatusOK, `{"id":"proof-1"}`)
			})
			b := &Bot{DB: db, API: api}
			err := test.commit(b, t.Context(), incoming{owner: "alice", chat: 1}, test.item)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.EqualError(t, err, core.ErrDatabase.Error())
			require.Positive(t, requests.Load(), "the promotion or command boundary precedes the write")
			require.Equal(t, requests.Load(), atConnect.Load(),
				"no command execution, domain retry or render may follow a failed receipt write")
		})
	}
}

func TestMediaRenderDatabaseNoRowsAndStoredDataControls(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	var foreign atomic.Int32
	b := &Bot{DB: db, API: mediaRenderAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/orders") {
			mediaRenderReply(w, http.StatusOK, `{}`)
			return
		}
		foreign.Add(1)
		mediaRenderReply(w, http.StatusNotFound, `{"code":"not_found"}`)
	})}
	_, err := db.Exec(t.Context(), `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,av_kind)
 VALUES('m','alice',1,'a','')`)
	require.NoError(t, err)
	hint := agent.MediaHint{ID: "m", CanInspect: true}
	require.NoError(t, b.avHint(t.Context(), "alice", &hint))
	require.False(t, hint.CanInspect, "missing AV result disables inspection")
	visible, err := b.passMediaTextVisible(t.Context(), "alice", "m")
	require.NoError(t, err)
	require.False(t, visible, "missing reply reference is invisible")
	for _, content := range []string{`"private-incompatible"`, `null`, `{"after":"private-incompatible"}`} {
		_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('alice',1,$1,$2) ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`,
			mediaModelReplyKind, content)
		require.NoError(t, err)
		visible, err = b.passMediaTextVisible(t.Context(), "alice", "m")
		require.Error(t, err)
		require.False(t, visible)
		require.False(t, core.IsDatabaseFailure(err), "incompatible persisted reference is not SQL failure")
		require.NotContains(t, err.Error(), "private-incompatible")
	}
	first, err := b.mediaButton(
		t.Context(),
		"alice",
		"m",
		mediaOrderChoice,
		agent.MediaCandidate{OrderID: "o"},
		"label",
	)
	require.NoError(t, err)
	again, err := b.mediaButton(
		t.Context(),
		"alice",
		"m",
		mediaOrderChoice,
		agent.MediaCandidate{OrderID: "o"},
		"label",
	)
	require.NoError(t, err)
	require.Equal(t, first, again, "existing button token is reused")
	food, err := b.foodButton(t.Context(), "alice", "label", foodButtonCommand{MediaID: "m"})
	require.NoError(t, err)
	var stored foodButtonCommand
	require.NoError(t, db.QueryRow(t.Context(), `SELECT command FROM bot.food_buttons WHERE owner='alice' AND token=$1`,
		strings.TrimPrefix(food.Data, foodPrefix)).Scan(&stored))
	require.Equal(t, foodButtonCommand{MediaID: "m"}, stored)
	_, err = b.mediaCandidates(t.Context(), "alice")
	require.False(t, core.IsDatabaseFailure(err))
	require.Positive(t, foreign.Load(), "missing pending proof continues to the registration boundary")
	_, err = db.Exec(t.Context(), `INSERT INTO bot.proof_pending(owner,command,selected_update)
 VALUES('alice','"private-incompatible"',1)`)
	require.NoError(t, err)
	requested := foreign.Load()
	_, err = b.mediaCandidates(t.Context(), "alice")
	var incompatible *json.UnmarshalTypeError
	require.ErrorAs(t, err, &incompatible)
	require.False(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "private-incompatible")
	require.Equal(t, requested, foreign.Load(), "undecodable pending proof stops before later boundaries")
}

func TestMediaRenderDatabaseTransportFaults(t *testing.T) {
	t.Parallel()
	t.Run("AV rejection cleanup", func(t *testing.T) {
		t.Parallel()
		db := foodPendingDatabase(t)
		_, err := db.Exec(t.Context(), `INSERT INTO bot.media_intake(id,owner,update_id,attachment_id,av_kind)
 VALUES('m','alice',1,'a','video')`)
		require.NoError(t, err)
		_, err = db.Exec(t.Context(),
			`INSERT INTO bot.av_results(intake_id,status,duration_num,duration_den,private_result)
 VALUES('m','ready',1,1,'{}')`)
		require.NoError(t, err)
		broken, fault := mediaRenderFaultyPool(t, db,
			"UPDATE bot.av_results a SET private_result=NULL FROM bot.media_intake m")
		rejected := mediaRenderAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			mediaRenderReply(w, http.StatusNotFound, `{"code":"media_not_found"}`)
		})
		b := &Bot{DB: broken, AV: mediaRenderAV{}, API: rejected}
		hint := agent.MediaHint{ID: "m"}
		require.ErrorIs(t, b.avHint(t.Context(), "alice", &hint), core.ErrDatabase)
		require.True(t, fault.fired)
		require.NoError(t, fault.closeError)
		require.False(t, hint.CanInspect)
		var retained bool
		require.NoError(t, db.QueryRow(t.Context(),
			`SELECT private_result IS NOT NULL FROM bot.av_results WHERE intake_id='m'`).Scan(&retained))
		require.True(t, retained, "failed cleanup is reported, not assumed")
		b.DB = db
		hint = agent.MediaHint{ID: "m"}
		require.NoError(t, b.avHint(t.Context(), "alice", &hint))
		require.False(t, hint.CanInspect)
		require.NoError(t, db.QueryRow(t.Context(),
			`SELECT private_result IS NOT NULL FROM bot.av_results WHERE intake_id='m'`).Scan(&retained))
		require.False(t, retained)
	})
	t.Run("food button insert", func(t *testing.T) {
		t.Parallel()
		db := foodPendingDatabase(t)
		broken, fault := mediaRenderFaultyPool(t, db, "INSERT INTO bot.food_buttons")
		b := &Bot{DB: broken}
		_, err := b.foodButton(t.Context(), "alice", "label", foodButtonCommand{MediaID: "m"})
		require.ErrorIs(t, err, core.ErrDatabase)
		require.True(t, fault.fired)
		require.NoError(t, fault.closeError)
		var buttons int
		require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM bot.food_buttons WHERE owner='alice'`).
			Scan(&buttons))
		require.Zero(t, buttons)
	})
	t.Run("pending proof read", func(t *testing.T) {
		t.Parallel()
		db := foodPendingDatabase(t)
		broken, fault := mediaRenderFaultyPool(t, db, "SELECT command FROM bot.proof_pending")
		b := &Bot{DB: broken, API: mediaRenderAPI(t, func(w http.ResponseWriter, _ *http.Request) {
			mediaRenderReply(w, http.StatusOK, `{}`)
		})}
		_, err := b.mediaCandidates(t.Context(), "alice")
		require.ErrorIs(t, err, core.ErrDatabase)
		require.True(t, fault.fired)
		require.NoError(t, fault.closeError)
	})
}
