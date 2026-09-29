package sandbox_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDeliverySurvivesRestart(t *testing.T) {
	t.Parallel()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for durable fake restart")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "sandbox_delivery_" + rand.Text()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		require.NoError(t, dropErr)
	})
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	_, err = db.Exec(
		t.Context(),
		`CREATE SCHEMA bot; CREATE TABLE bot.fake_state(id bool PRIMARY KEY, data jsonb NOT NULL); CREATE TABLE bot.cursors(name text PRIMARY KEY,value bigint NOT NULL)`,
	)
	require.NoError(t, err)
	first, err := sandbox.New(t.Context(), db, "sandbox")
	require.NoError(t, err)
	server := httptest.NewServer(first.Handler())
	client := telegram.Client{Base: server.URL, Token: "sandbox"}
	var sent telegram.Message
	err = client.Call(
		t.Context(),
		"sendMessage",
		map[string]any{
			"chat_id":           "@sandbox_forum",
			"message_thread_id": 101,
			"parse_mode":        "HTML",
			"text":              "😀 <b>persisted</b>",
		},
		&sent,
	)
	server.Close()
	require.NoError(t, err)
	restored, err := sandbox.New(t.Context(), db, "sandbox")
	require.NoError(t, err)
	response := httptest.NewRecorder()
	restored.Handler().
		ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/lab/state?user=-1009002", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var state struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	require.Equal(t, []telegram.Message{sent}, state.Messages)
	require.Equal(t, int64(101), state.Messages[0].ThreadID)
	require.Equal(t, "sandbox_forum", state.Messages[0].Chat.Username)
	require.Equal(t, []telegram.MessageEntity{{Type: "bold", Offset: 3, Length: 9}}, state.Messages[0].Entities)
}
