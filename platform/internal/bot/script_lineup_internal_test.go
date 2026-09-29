package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func lineupScriptFixture(t *testing.T, csv string) *Bot {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lineup.csv")
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))
	source, err := agent.LoadLineup(path, 2026, "Europe/Minsk")
	require.NoError(t, err)
	return &Bot{Lineup: source}
}

func runLineupScript(t *testing.T, b *Bot, input *agent.Input, code string) (json.RawMessage, error) {
	t.Helper()
	entry := b.scriptLineupEntry()
	return scriptworker.Execute(t.Context(), scriptprotocol.ExecuteRequest{
		Code: code, Input: json.RawMessage(`{}`), Tools: []scriptprotocol.Tool{entry.descriptor},
	}, func(ctx context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
		if call.Name == "$help" {
			return json.Marshal(entry.descriptor)
		}
		if call.Name == "$list" {
			return json.Marshal([]scriptprotocol.Tool{entry.descriptor})
		}
		record, err := entry.prepare(ctx, "owner", 1, call, *input)
		if err != nil {
			return nil, err
		}
		result, err := entry.execute(ctx, "owner", call, record, input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	})
}

func TestScriptLineupSobekFiltersAndHostTime(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"English DJ", "Диджей Ёж 🎶"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			b := lineupScriptFixture(t, "time,\"Fri, 25.09\"\nroom,Зал\n01:00,"+label+"\n")
			now := time.Date(2026, time.September, 25, 22, 30, 0, 0, time.UTC)
			input := agent.Input{LineupSource: b.Lineup.Snapshot(now)}
			for _, scope := range []string{"current", "day", "full"} {
				query, err := json.Marshal(
					agent.LineupQuery{Scope: scope, Date: "2026-09-25", Room: "зАЛ", DJ: strings.ToLower(label)},
				)
				require.NoError(t, err)
				result, err := runLineupScript(t, b, &input, "return await tools.lineup.query("+string(query)+");")
				require.NoError(t, err)
				var read agent.LineupRead
				require.NoError(t, json.Unmarshal(result, &read))
				require.Len(t, read.Entries, 1)
				assert.Equal(t, label, read.Entries[0].DJ)
				assert.Equal(t, "2026-09-25", read.Entries[0].EventDate)
				assert.Equal(t, now.Unix(), read.AsOf.Unix())
				assert.Equal(t, "+03:00", read.AsOf.Format("Z07:00"))
			}
			assert.Equal(t, 1, input.LineupSource.Remaining)
		})
	}
}

func TestScriptLineupSobekContinuationAndSharedBudget(t *testing.T) {
	t.Parallel()
	var csv strings.Builder
	csv.WriteString("time,\"Fri, 25.09\"\nroom,Main\n")
	for index := range 500 {
		fmt.Fprintf(&csv, "22:00,DJ-%03d\n", index)
	}
	b := lineupScriptFixture(t, csv.String())
	input := agent.Input{LineupSource: b.Lineup.Snapshot(time.Now())}
	code := `let q={scope:"full"}, count=0, last=""; for(let i=0;i<4;i++){let p=await tools.lineup.query(q); count+=p.entries.length; last=p.entries[p.entries.length-1].dj; q.cursor=p.next_cursor; if(!p.omitted)break;} return {count,last,cursor:q.cursor};`
	result, err := runLineupScript(t, b, &input, code)
	require.NoError(t, err)
	var page struct {
		Count  int    `json:"count"`
		Last   string `json:"last"`
		Cursor string `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal(result, &page))
	assert.Positive(t, page.Count)
	assert.Less(t, page.Count, 500)
	assert.Equal(t, fmt.Sprintf("DJ-%03d", page.Count-1), page.Last)
	require.NotEmpty(t, page.Cursor)
	assert.Zero(t, input.LineupSource.Remaining)
	_, err = runLineupScript(t, b, &input, `return await tools.lineup.query({scope:"full"});`)
	require.Error(t, err)
	next := agent.Input{LineupSource: b.Lineup.Snapshot(time.Now().Add(24 * time.Hour))}
	query := agent.LineupQuery{Scope: "full", Cursor: page.Cursor}
	for {
		args, marshalErr := json.Marshal(query)
		require.NoError(t, marshalErr)
		result, err = runLineupScript(t, b, &next, "return await tools.lineup.query("+string(args)+");")
		require.NoError(t, err)
		var read agent.LineupRead
		require.NoError(t, json.Unmarshal(result, &read))
		for offset, entry := range read.Entries {
			assert.Equal(t, fmt.Sprintf("DJ-%03d", page.Count+offset), entry.DJ)
		}
		page.Count += len(read.Entries)
		if !read.Omitted {
			break
		}
		query.Cursor = read.NextCursor
	}
	assert.Equal(t, 500, page.Count)
	require.Positive(t, next.LineupSource.Remaining)
	query.Room = "Elsewhere"
	args, err := json.Marshal(query)
	require.NoError(t, err)
	_, err = runLineupScript(t, b, &next, "return await tools.lineup.query("+string(args)+");")
	require.Error(t, err)
}

func TestScriptLineupValidationAndDiscovery(t *testing.T) {
	t.Parallel()
	b := &Bot{}
	input := agent.Input{LineupSource: b.Lineup.Snapshot(time.Now())}
	result, err := runLineupScript(
		t,
		b,
		&input,
		`const h=await tools.lineup.query.$help(); const l=await tools.$list(); return {name:h.name, listed:l[0].name, page:await tools.lineup.query({scope:"full"})};`,
	)
	require.NoError(t, err)
	assert.Contains(t, string(result), `"unavailable"`)
	assert.Contains(t, string(result), `"name":"lineup.query"`)
	for _, raw := range []string{`{"scope":"other"}`, `{"scope":"full","date":"2026-02-30"}`, `{"scope":"full","path":"/tmp/private"}`, `{"scope":"full","now":"2026-01-01"}`} {
		_, err = b.scriptLineupEntry().
			prepare(t.Context(), "owner", 1, scriptclient.ToolCall{Name: scriptLineupQuery, Arguments: json.RawMessage(raw)}, input)
		require.Error(t, err)
	}
	projection := scriptCallProjection(
		agent.ScriptToolResult{Name: scriptLineupQuery, Result: json.RawMessage(`{"entries":["private prompt data"]}`)},
	)
	assert.Contains(t, string(projection.Result), `"payload_omitted":true`)
}
