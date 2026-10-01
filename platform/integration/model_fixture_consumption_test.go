package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

const consumptionControlKey = "synthetic-model-control-key-only"
const consumptionScope = "?owner=alice&update_id=9&turn=0"

type consumptionState struct {
	Owner          string `json:"owner"`
	UpdateID       int64  `json:"update_id"`
	Turn           int    `json:"turn"`
	Mode           string `json:"mode"`
	Phase          string `json:"phase"`
	Consumed       bool   `json:"consumed"`
	Durable        bool   `json:"durable"`
	RequestSHA256  string `json:"request_sha256"`
	ResponseSHA256 string `json:"response_sha256"`
}

func consumptionHTTP(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	request.Header.Set("X-R104-Control", consumptionControlKey)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(raw)
}

func consumptionProvider(t *testing.T, db *pgxpool.Pool) (*httptest.Server, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	f, err := sandbox.New(ctx, db, "synthetic-model-token")
	require.NoError(t, err)
	server := httptest.NewServer(f.Handler())
	t.Cleanup(server.Close)
	return server, cancel
}

func waitConsumptionState(t *testing.T, phase string) consumptionState {
	t.Helper()
	var state consumptionState
	require.Eventually(t, func() bool {
		status, body := consumptionHTTP(
			t,
			http.MethodGet,
			"http://127.0.0.1:8090/control/model/state"+consumptionScope,
			"",
		)
		if status != http.StatusOK {
			return false
		}
		require.NoError(t, json.Unmarshal([]byte(body), &state))
		return state.Phase == phase
	}, 3*time.Second, 10*time.Millisecond)
	return state
}

func stopConsumptionProvider(t *testing.T, server *httptest.Server, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	server.Close()
	require.Eventually(t, func() bool {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:8090", 50*time.Millisecond)
		if err != nil {
			return true
		}
		_ = connection.Close()
		return false
	}, time.Second, 10*time.Millisecond)
}

const consumptionInstall = `{"owner":"alice","update_id":9,"hold":{"turn":0,"mode":"after_consume"},"steps":[{"expect":{"text":"private consumption marker"},"plan":{"view":"workflow","text":"private generated answer"}}]}`

// Serial: the real reserved listener and its environment belong to this case.
// This proves provider durability, not a business saved-plan/effect observation.
func TestModelConsumptionDurableRebootAndDeniedReinstallation(t *testing.T) {
	db := database(t)
	firstJournal := filepath.Join(t.TempDir(), "generation-a.jsonl")
	t.Setenv("R104_CONTROL_KEY", consumptionControlKey)
	t.Setenv("R104_JOURNAL", firstJournal)
	server, cancel := consumptionProvider(t, db)
	status, body := consumptionHTTP(t, http.MethodPost, server.URL+"/lab/model/fixtures", consumptionInstall)
	require.Equal(t, http.StatusCreated, status, body)
	result := make(chan error, 1)
	go func() {
		_, planErr := (sandbox.FixtureRemote{URL: server.URL + "/lab/model"}).Plan(
			agent.WithRequestScope(
				t.Context(),
				agent.RequestScope{Owner: "alice", UpdateID: 9},
			),
			agent.Input{Text: "private consumption marker"},
		)
		result <- planErr
	}()
	state := waitConsumptionState(t, "consumed_held")
	assert.True(t, state.Consumed)
	assert.True(t, state.Durable)
	assert.Equal(t, "alice", state.Owner)
	assert.EqualValues(t, 9, state.UpdateID)
	assert.Zero(t, state.Turn)
	assert.Len(t, state.RequestSHA256, 64)
	assert.Len(t, state.ResponseSHA256, 64)
	var saved []byte
	require.NoError(t, db.QueryRow(t.Context(), `SELECT data FROM bot.fake_state WHERE id=true`).Scan(&saved))
	assert.NotContains(t, string(saved), "private consumption marker")
	assert.NotContains(t, string(saved), "private generated answer")
	select {
	case err := <-result:
		t.Fatalf("response escaped consumed barrier: %v", err)
	default:
	}
	stopConsumptionProvider(t, server, cancel)
	require.Error(t, <-result)
	oldJournal, err := os.ReadFile(firstJournal)
	require.NoError(t, err)
	oldSHA := sha256.Sum256(oldJournal)
	_, err = sandbox.New(t.Context(), db, "synthetic-model-token")
	require.Error(t, err, "old exclusive journal cannot be reopened")
	secondJournal := filepath.Join(filepath.Dir(firstJournal), "generation-b.jsonl")
	t.Setenv("R104_JOURNAL", secondJournal)
	reopened, cancel2 := consumptionProvider(t, db)
	state = waitConsumptionState(t, "consumed_unavailable")
	assert.True(t, state.Consumed)
	assert.True(t, state.Durable)
	status, body = consumptionHTTP(t, http.MethodPost, reopened.URL+"/lab/model/fixtures", consumptionInstall)
	assert.Equal(t, http.StatusConflict, status, body)
	_, err = (sandbox.FixtureRemote{URL: reopened.URL + "/lab/model"}).Plan(
		agent.WithRequestScope(
			t.Context(),
			agent.RequestScope{Owner: "alice", UpdateID: 9},
		),
		agent.Input{Text: "private consumption marker"},
	)
	require.Error(t, err, "reboot must not fabricate the prior unsaved result")
	status, body = consumptionHTTP(
		t,
		http.MethodPost,
		reopened.URL+"/lab/model/fixtures",
		`{"owner":"alice","update_id":10,"steps":[{"expect":{"text":"new intent"},"plan":{"view":"workflow","text":"new generated answer"}}]}`,
	)
	require.Equal(t, http.StatusCreated, status, body)
	plan, err := (sandbox.FixtureRemote{URL: reopened.URL + "/lab/model"}).Plan(
		agent.WithRequestScope(
			t.Context(),
			agent.RequestScope{Owner: "alice", UpdateID: 10},
		),
		agent.Input{Text: "new intent"},
	)
	require.NoError(t, err)
	assert.Equal(t, "new generated answer", plan.Text)
	stopConsumptionProvider(t, reopened, cancel2)
	after, err := os.ReadFile(firstJournal)
	require.NoError(t, err)
	assert.Equal(t, oldSHA, sha256.Sum256(after))
	newJournal, err := os.ReadFile(secondJournal)
	require.NoError(t, err)
	t.Logf("private journal generations a/b retained; SHA256 %x / %x", oldSHA, sha256.Sum256(newJournal))
}

func TestModelConsumptionSQLDeadlineDoesNotConfirmOrRewind(t *testing.T) {
	t.Setenv("R104_CONTROL_KEY", consumptionControlKey)
	for _, persistFence := range []bool{false, true} {
		name := "immediate_reboot"
		if persistFence {
			name = "later_save_then_reboot"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("R104_CONTROL_KEY", consumptionControlKey)
			modelConsumptionSQLDeadlineReboot(t, persistFence)
		})
	}
}

func modelConsumptionSQLDeadlineReboot(t *testing.T, persistFence bool) {
	t.Helper()
	db := database(t)
	_, err := db.Exec(t.Context(), `INSERT INTO bot.fake_state(id,data) VALUES(true,'{}')`)
	require.NoError(t, err)
	t.Setenv("R104_CONTROL_KEY", consumptionControlKey)
	t.Setenv("R104_JOURNAL", filepath.Join(t.TempDir(), "generation-a.jsonl"))
	server, cancel := consumptionProvider(t, db)
	status, body := consumptionHTTP(t, http.MethodPost, server.URL+"/lab/model/fixtures", consumptionInstall)
	require.Equal(t, http.StatusCreated, status, body)
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = tx.Exec(t.Context(), `LOCK TABLE bot.fake_state IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	result := make(chan error, 1)
	started := time.Now()
	go func() {
		_, planErr := (sandbox.FixtureRemote{URL: server.URL + "/lab/model"}).Plan(
			agent.WithRequestScope(
				t.Context(),
				agent.RequestScope{Owner: "alice", UpdateID: 9},
			),
			agent.Input{Text: "private consumption marker"},
		)
		result <- planErr
	}()
	state := waitConsumptionState(t, "persistence_pending")
	assert.False(t, state.Consumed)
	assert.False(t, state.Durable)
	status, _ = consumptionHTTP(
		t,
		http.MethodPost,
		"http://127.0.0.1:8090/control/model/release"+consumptionScope,
		`{"action":"deliver"}`,
	)
	assert.Equal(t, http.StatusConflict, status)
	select {
	case planErr := <-result:
		require.Error(t, planErr)
	case <-time.After(4 * time.Second):
		t.Fatal("provider SQL did not terminate")
	}
	assert.Less(t, time.Since(started), 4*time.Second)
	state = waitConsumptionState(t, "persistence_unavailable")
	assert.False(t, state.Consumed)
	assert.False(t, state.Durable)
	status, body = consumptionHTTP(t, http.MethodGet, server.URL+"/lab/model/state?owner=alice&update_id=9", "")
	require.Equal(t, http.StatusOK, status, body)
	var counts struct {
		Accepted int `json:"accepted"`
		NextTurn int `json:"next_turn"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &counts))
	assert.Zero(t, counts.Accepted)
	assert.Zero(t, counts.NextTurn)
	status, _ = consumptionHTTP(t, http.MethodPost, server.URL+"/lab/model/fixtures", consumptionInstall)
	assert.Equal(t, http.StatusConflict, status, "an uncertain fence remains rejected in this process")
	require.NoError(t, tx.Rollback(t.Context()))
	if persistFence {
		// A later ordinary successful save retains the uncertain rejection fence.
		status, body = consumptionHTTP(
			t,
			http.MethodPost,
			server.URL+"/lab/model/fixtures",
			`{"input":{"user":101,"text":"new intent"},"steps":[{"expect":{"text":"new intent"},"plan":{"view":"workflow","text":"new answer"}}]}`,
		)
		require.Equal(t, http.StatusCreated, status, body)
	}
	stopConsumptionProvider(t, server, cancel)
	oldJournal, err := os.ReadFile(os.Getenv("R104_JOURNAL"))
	require.NoError(t, err)
	oldSHA := sha256.Sum256(oldJournal)
	oldPath := os.Getenv("R104_JOURNAL")
	t.Setenv("R104_JOURNAL", filepath.Join(filepath.Dir(oldPath), "generation-b.jsonl"))
	reopened, cancel2 := consumptionProvider(t, db)
	if persistFence {
		state = waitConsumptionState(t, "consumed_unavailable")
		assert.True(t, state.Durable)
		status, body = consumptionHTTP(t, http.MethodPost, reopened.URL+"/lab/model/fixtures", consumptionInstall)
		assert.Equal(t, http.StatusConflict, status, body)
	} else {
		status, _ = consumptionHTTP(t, http.MethodGet,
			"http://127.0.0.1:8090/control/model/state"+consumptionScope, "")
		assert.Equal(t, http.StatusNotFound, status, "failed SQL cannot create durable evidence")
	}
	_, err = (sandbox.FixtureRemote{URL: reopened.URL + "/lab/model"}).Plan(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
		agent.Input{Text: "private consumption marker"})
	require.Error(t, err)
	if !persistFence {
		// No irreversible advance occurred. A new installation is a first consumption,
		// not replay of a consumed step; no body was restored by the reboot.
		installation := strings.Replace(consumptionInstall, `"hold":{"turn":0,"mode":"after_consume"},`, "", 1)
		status, body = consumptionHTTP(t, http.MethodPost, reopened.URL+"/lab/model/fixtures", installation)
		require.Equal(t, http.StatusCreated, status, body)
		remote := sandbox.FixtureRemote{URL: reopened.URL + "/lab/model"}
		ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
		_, err = remote.Plan(ctx, agent.Input{Text: "private consumption marker"})
		require.NoError(t, err)
		_, err = remote.Plan(ctx, agent.Input{Text: "private consumption marker"})
		require.Error(t, err, "the first actual consumption remains one-shot")
	}
	stopConsumptionProvider(t, reopened, cancel2)
	after, err := os.ReadFile(oldPath)
	require.NoError(t, err)
	assert.Equal(t, oldSHA, sha256.Sum256(after))
}
