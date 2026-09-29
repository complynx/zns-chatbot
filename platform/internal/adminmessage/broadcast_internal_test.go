package adminmessage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastGoTemplateEscapingAndBudgets(t *testing.T) {
	t.Parallel()
	fields := map[string]any{
		"first_name":         "<Alex & Co>",
		"user_link":          `<a href="tg://user?id=1000000000">&lt;Alex&gt;</a>`,
		"known_names":        []any{"A", "B"},
		"user_link_username": "", "user_name": "<Alex>",
		"user_id": int64(1000000000),
	}
	for _, mode := range []string{"", "Markdown", "HTML"} {
		renderer, err := parseBroadcastTemplate(
			Content{Text: `{{.first_name}} / {{template "user_link" .}}`, ParseMode: mode},
		)
		require.NoError(t, err)
		content, err := renderer.render(fields, Content{ParseMode: mode})
		require.NoError(t, err)
		if mode == "HTML" {
			assert.Contains(t, content.Text, "&lt;Alex &amp; Co&gt;")
		} else {
			assert.Contains(t, content.Text, "<Alex & Co>")
		}
		assert.Contains(t, content.Text, `<a href="tg://user?id=1000000000">&lt;Alex&gt;</a>`)
	}
	for _, raw := range []string{`{{.missing}}`, `{{index . "missing"}}`, `{{range .user_id}}{{end}}`} {
		renderer, err := parseBroadcastTemplate(Content{Text: raw})
		require.NoError(t, err)
		_, err = renderer.render(fields, Content{})
		require.Error(t, err)
	}
	for _, raw := range []string{`{{printf "%1000000000s" "x"}}`, `{{with .known_names}}{{range .user_id}}{{end}}{{end}}`, `{{define "loop"}}{{template "loop"}}{{end}}{{template "loop"}}`} {
		_, err := parseBroadcastTemplate(Content{Text: raw})
		require.Error(t, err)
	}
	renderer, err := parseBroadcastTemplate(Content{Text: `{{range .known_names}}{{.}}{{end}}`})
	require.NoError(t, err)
	content, err := renderer.render(fields, Content{})
	require.NoError(t, err)
	assert.Equal(t, "AB", content.Text)
}

func TestBroadcastHTMLUsernameLinkEscapesName(t *testing.T) {
	t.Parallel()
	renderer, err := parseBroadcastTemplate(Content{Text: `{{template "user_link" .}}`, ParseMode: "HTML"})
	require.NoError(t, err)
	content, err := renderer.render(map[string]any{
		"user_id": int64(101), "user_link_username": "alice", "user_name": `<Alice & "friends">`,
	}, Content{ParseMode: "HTML"})
	require.NoError(t, err)
	assert.Equal(t, `<a href="https://t.me/alice">&lt;Alice &amp; &#34;friends&#34;&gt;</a>`, content.Text)
}

func TestBroadcastSelectorPresenceAndPrecision(t *testing.T) {
	t.Parallel()
	fields := map[string]any{
		"first_name":         nil,
		"last_name":          "",
		"user_id":            json.Number("9007199254740993"),
		"known_names":        []any{"A", "B"},
		"user_link_username": "", "user_name": "<Alex>",
	}
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{`{"first_name":null}`, true}, {`{"informal_name":null}`, true}, {`{"informal_name":{"$ne":null}}`, false}, {`{"first_name":{"$exists":true}}`, true}, {`{"informal_name":{"$exists":false}}`, true},
		{`{"last_name":null}`, false}, {`{"user_id":9007199254740993}`, true}, {`{"user_id":9007199254740992}`, false}, {`{"known_names":"A"}`, true}, {`{"user_id":{"$gt":9007199254740992}}`, true},
	} {
		predicate, err := parseSelector(test.raw)
		require.NoError(t, err)
		assert.Equal(t, test.want, matchesSelector(fields, predicate), test.raw)
	}
	for _, raw := range []string{`{"$where":"anything"}`, `{"name.x":1}`, `{"first_name":{"$regex":".*"}}`} {
		_, err := parseSelector(raw)
		require.Error(t, err)
	}
}
