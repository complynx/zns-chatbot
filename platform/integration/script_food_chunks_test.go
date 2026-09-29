package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestScriptFoodViewChunksAndChangedMenu(t *testing.T) {
	t.Parallel()
	f, _ := foodBotFixture(t)
	title := strings.Repeat("Суп", 1800)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.food_events SET menu=jsonb_set(menu,'{friday,lunch,0,title_ru}',to_jsonb($1::text)) WHERE event_id='food-bot'`,
		title,
	)
	require.NoError(t, err)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				var first core.ReadChunk
				raw := scriptCall(ctx, t, callback, "food.view", `{}`)
				require.LessOrEqual(t, len(raw), core.ReadPageBytes)
				require.NoError(t, json.Unmarshal(raw, &first))
				require.True(t, first.More)
				args, marshalErr := json.Marshal(map[string]string{"cursor": first.NextCursor})
				require.NoError(t, marshalErr)
				var second core.ReadChunk
				raw = scriptCall(ctx, t, callback, "food.view", string(args))
				require.NoError(t, json.Unmarshal(raw, &second))
				require.False(t, second.More)
				assert.True(t, json.Valid([]byte(first.JSON+second.JSON)))
				assert.Contains(t, first.JSON+second.JSON, title)
				_, updateErr := f.db.Exec(
					ctx,
					`UPDATE core.food_events SET menu=jsonb_set(menu,'{friday,lunch,0,title_en}','"Changed"') WHERE event_id='food-bot'`,
				)
				require.NoError(t, updateErr)
				stale := scriptCall(ctx, t, callback, "food.view", string(args))
				assert.JSONEq(t, `{"error":"stale","restart":true}`, string(stale))
				_, callErr := callback(
					ctx,
					scriptclient.ToolCall{Name: "food.change", Arguments: json.RawMessage(`{"name":"delete_meals"}`)},
				)
				require.Error(t, callErr)
				return json.RawMessage(`{"stale":true}`), nil
			},
		),
	)
}
