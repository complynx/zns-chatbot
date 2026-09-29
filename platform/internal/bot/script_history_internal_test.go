package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestHistoryReadSobekChunksAndPrivacy(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("я🙂", 3500)
	generation := int64(0)
	textReads := 0
	deleteDuringRead := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer delegated-token", r.Header.Get("Authorization"))
		if r.URL.Path == "/v1/me/history/generation" {
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]int64{"generation": generation}))
			return
		}
		assert.Equal(t, "/v1/me/history/7/text", r.URL.Path)
		textReads++
		if deleteDuringRead {
			generation++
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		assert.NoError(t, err)
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		assert.NoError(t, err)
		assert.LessOrEqual(t, limit, conversation.MaxChunkCharacters)
		if offset > 0 {
			assert.Equal(t, "fingerprint", r.URL.Query().Get("digest"))
		}
		runes := []rune(text)
		end := min(offset+limit, len(runes))
		assert.NoError(
			t,
			json.NewEncoder(w).
				Encode(conversation.TextChunk{EventID: 7, Digest: "fingerprint", Offset: offset, Total: len(runes), Text: string(runes[offset:end]), More: end < len(runes), NextOffset: end, Generation: generation}),
		)
	}))
	t.Cleanup(server.Close)
	b := Bot{
		API: appclient.Client{
			Base:     server.URL,
			Exchange: &authExchange{},
			Links: authLinks{
				user: identity.User{Owner: "alice", Subject: "z-alice"},
			},
			SandboxToken: (identity.Signer{}).Token,
		},
	}
	ctx, owner, err := b.API.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	entry := b.historyReadEntry()
	request := scriptprotocol.ExecuteRequest{
		Input: json.RawMessage(`{}`),
		Tools: []scriptprotocol.Tool{entry.Descriptor},
		Code:  `let cursor="",text="",count=0; do {const page=await tools.history.read({event_id:7,cursor}); text+=page.text; cursor=page.next_cursor;count++;}while(cursor);return {text,count};`,
	}
	result, err := scriptworker.Execute(
		ctx,
		request,
		func(ctx context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
			record, callbackErr := entry.Prepare(ctx, owner, 1, call, agent.Input{})
			if callbackErr != nil {
				return nil, callbackErr
			}
			value, callbackErr := entry.Execute(ctx, owner, call, record, &agent.Input{})
			if callbackErr != nil {
				return nil, callbackErr
			}
			raw, callbackErr := json.Marshal(value)
			require.LessOrEqual(t, len(raw), maxScriptReadBytes)
			return raw, callbackErr
		},
	)
	require.NoError(t, err)
	var output struct {
		Text  string `json:"text"`
		Count int    `json:"count"`
	}
	require.NoError(t, json.Unmarshal(result, &output))
	require.Equal(t, text, output.Text)
	require.Equal(t, 2, output.Count)
	cursor := encodeScriptCursor(
		scriptReadCursor{Owner: owner, Kind: scriptHistoryRead, Scope: "7", Offset: 6000, Digest: "fingerprint"},
	)
	raw, err := json.Marshal(scriptHistoryArguments{EventID: 7, Cursor: cursor})
	require.NoError(t, err)
	generation = 1
	_, err = entry.Execute(
		ctx,
		owner,
		scriptprotocol.ToolCall{Name: scriptHistoryRead, Arguments: raw},
		agenthost.ScriptToolRecord{},
		&agent.Input{},
	)
	require.ErrorIs(t, err, appclient.ErrReadStale)
	require.Equal(t, 2, textReads)
	_, err = entry.Execute(
		ctx,
		"bob",
		scriptprotocol.ToolCall{Name: scriptHistoryRead, Arguments: raw},
		agenthost.ScriptToolRecord{},
		&agent.Input{},
	)
	require.Error(t, err)
	require.Equal(t, 2, textReads)
	deleteDuringRead = true
	_, err = entry.Execute(
		ctx,
		owner,
		scriptprotocol.ToolCall{Name: scriptHistoryRead, Arguments: json.RawMessage(`{"event_id":7}`)},
		agenthost.ScriptToolRecord{},
		&agent.Input{},
	)
	require.ErrorIs(t, err, appclient.ErrReadStale)
}

func TestHistoryDeletionCannotResurrectCompletedOutput(t *testing.T) {
	t.Parallel()
	record := agenthost.ScriptRecord{
		HistoryGeneration: 1,
		Request:           agent.ScriptProposal{Code: "private"},
		Run:               agent.ScriptRun{Result: json.RawMessage(`"private"`)},
	}
	require.True(t, agenthost.RedactHistoryScript(&record, 2))
	record.Run = agent.ScriptRun{Result: json.RawMessage(`"late private result"`)}
	require.True(t, agenthost.RedactHistoryScript(&record, 2))
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private result")
	require.Contains(t, string(encoded), historyDeleted)
	pages := []conversation.Page{{Generation: 1, Events: []conversation.Event{{Text: "private"}}}}
	agenthost.RedactHistoryPages(pages, 2)
	require.Empty(t, pages[0].Events)
	require.Equal(t, historyDeleted, pages[0].Error)
}

func TestHistoryDeletionFencesConcurrentScriptCompletion(t *testing.T) {
	t.Parallel()
	readComplete := make(chan struct{})
	allowCompletion := make(chan struct{})
	finished := make(chan agenthost.ScriptRecord)
	go func() {
		record := agenthost.ScriptRecord{HistoryGeneration: 1}
		run := agent.ScriptRun{Result: json.RawMessage(`{"derived":"private canary transformed"}`)}
		close(readComplete)
		<-allowCompletion
		(&Bot{}).scriptHost().Store.CompleteRecord(&record, run, knowledge.MemoryDeletionState{}, 2)
		finished <- record
	}()
	<-readComplete
	close(allowCompletion)
	record := <-finished
	require.True(t, record.HistoryRedacted)
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private canary")
	require.Contains(t, string(raw), historyDeleted)
}

func TestHistoryReadSobekContinuesAcrossRunBudget(t *testing.T) {
	t.Parallel()
	original := strings.Repeat("я🙂\x01<", 10001) + "terminal-canary"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer delegated-token", r.Header.Get("Authorization"))
		if r.URL.Path == "/v1/me/history/generation" {
			_, _ = w.Write([]byte(`{"generation":0}`))
			return
		}
		assert.Equal(t, "/v1/me/history/9/text", r.URL.Path)
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		assert.NoError(t, err)
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		assert.NoError(t, err)
		runes := []rune(original)
		end := min(offset+limit, len(runes))
		assert.NoError(
			t,
			json.NewEncoder(w).
				Encode(conversation.TextChunk{EventID: 9, Digest: "stable", Offset: offset, Total: len(runes), Text: string(runes[offset:end]), More: end < len(runes), NextOffset: end}),
		)
	}))
	t.Cleanup(server.Close)
	b := Bot{
		API: appclient.Client{
			Base:     server.URL,
			Exchange: &authExchange{},
			Links: authLinks{
				user: identity.User{Owner: "alice", Subject: "z-alice"},
			},
			SandboxToken: (identity.Signer{}).Token,
		},
	}
	ctx, owner, err := b.API.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	entry := b.historyReadEntry()
	var reconstructed strings.Builder
	cursor := ""
	totalCalls := 0
	for turn := range 2 {
		input, marshalErr := json.Marshal(map[string]string{"cursor": cursor})
		require.NoError(t, marshalErr)
		request := scriptprotocol.ExecuteRequest{
			Input: input,
			Tools: []scriptprotocol.Tool{entry.Descriptor},
			Code:  `let cursor=input.cursor; for(let n=0;n<8;n++){const page=await tools.history.read({event_id:9,cursor});cursor=page.next_cursor;if(!page.more)return {cursor,more:false};}return {cursor,more:true};`,
		}
		calls := 0
		result, runErr := scriptworker.Execute(
			ctx,
			request,
			func(ctx context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
				calls++
				totalCalls++
				record, prepareErr := entry.Prepare(ctx, owner, 1, call, agent.Input{})
				if prepareErr != nil {
					return nil, prepareErr
				}
				value, readErr := entry.Execute(ctx, owner, call, record, &agent.Input{})
				if readErr != nil {
					return nil, readErr
				}
				chunk, ok := value.(agenthost.ScriptHistoryChunk)
				require.True(t, ok)
				reconstructed.WriteString(chunk.Text)
				raw, encodeErr := json.Marshal(value)
				require.LessOrEqual(t, len(raw), maxScriptReadBytes)
				return raw, encodeErr
			},
		)
		require.NoError(t, runErr)
		var continuation struct {
			Cursor string `json:"cursor"`
			More   bool   `json:"more"`
		}
		require.NoError(t, json.Unmarshal(result, &continuation))
		cursor = continuation.Cursor
		if turn == 0 {
			require.Equal(t, 8, calls)
			require.True(t, continuation.More)
			require.NotEmpty(t, cursor)
			require.NotContains(t, reconstructed.String(), "terminal-canary")
		} else {
			require.False(t, continuation.More)
			require.Empty(t, cursor)
		}
	}
	require.Greater(t, totalCalls, scriptprotocol.MaxCalls)
	require.Equal(t, original, reconstructed.String())
}
