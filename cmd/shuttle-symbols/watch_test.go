package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestDescriptorTransientReadBackoffResumesOriginalJournal(t *testing.T) {
	t.Parallel()
	a := arguments{journal: t.TempDir()}
	entry := db.SymbolRecord{Generation: uuid.New(), Reference: symbols.Reference{Kind: symbols.Sticker, ID: "same"}}
	directory := filepath.Join(a.journal, entry.Generation.String())
	require.NoError(t, os.Mkdir(directory, 0700))
	state := symboldescribe.State{ProcedureID: uuid.New(), Generation: entry.Generation, Reference: entry.Reference,
		Phase: "running", ChatID: "original-chat", ArtifactIDs: []string{"original-artifact"}, RunID: "original-run",
		Request: &loom.CreateRunRequest{ChatID: "original-chat", ExecutionProfile: loom.NativeFilesProfile}}
	require.NoError(t, save(filepath.Join(directory, "journal.json"), state))
	before, err := os.ReadFile(filepath.Join(directory, "journal.json"))
	require.NoError(t, err)
	calls := 0
	step := func(_ context.Context, got arguments) error {
		calls++
		require.Equal(t, entry.Generation, got.generation)
		require.Equal(t, directory, got.journal)
		if calls == 1 {
			return &telegram.Error{StatusCode: 429, Retryable: true, RetryAfter: time.Hour}
		}
		restored, readErr := descriptorState(got.journal)
		require.NoError(t, readErr)
		require.Equal(t, state, restored)
		return nil
	}
	done, err := advanceEntry(t.Context(), a, entry, step)
	require.NoError(t, err)
	require.False(t, done)
	retry, err := readDescriptorRetry(directory)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), retry.After, time.Second)
	// A new invocation reads the durable deadline and does not call the step.
	_, err = advanceEntry(t.Context(), a, entry, step)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	after, err := os.ReadFile(filepath.Join(directory, "journal.json"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	retry.After = time.Now().Add(-time.Second)
	require.NoError(t, saveDescriptorRetry(directory, retry))
	_, err = advanceEntry(t.Context(), a, entry, step)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	_, err = os.Stat(filepath.Join(directory, "retry.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(directory, "blocked.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDescriptorRecoveryRequiresSafeOriginalJournal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pending string
		cause         error
		missing       bool
	}{
		{name: "unknown upload", pending: "upload_artifact", cause: &loom.Error{Retryable: true, Outcome: loom.Unknown}},
		{name: "unknown chat", pending: "create_chat", cause: &loom.Error{Retryable: true, Outcome: loom.Unknown}},
		{name: "uncertain paid create", pending: "create_run", cause: &loom.Error{Retryable: true, Outcome: loom.Unknown}},
		{name: "unauthorized", cause: &telegram.Error{StatusCode: 401, Retryable: true}},
		{name: "forbidden", cause: &loom.Error{StatusCode: 403, Retryable: true}},
		{name: "invalid run receipt", cause: &loom.Error{Retryable: true, Outcome: loom.Rejected}},
		{name: "invalid result", cause: symbols.ErrInvalid},
		{name: "missing journal", missing: true, cause: &loom.Error{StatusCode: 503, Retryable: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := arguments{journal: t.TempDir()}
			entry := db.SymbolRecord{Generation: uuid.New(), Reference: symbols.Reference{Kind: symbols.Sticker, ID: "same"}}
			directory := filepath.Join(a.journal, entry.Generation.String())
			if !tc.missing {
				require.NoError(t, os.Mkdir(directory, 0700))
				require.NoError(t, save(filepath.Join(directory, "journal.json"), symboldescribe.State{
					ProcedureID: uuid.New(), Generation: entry.Generation, Reference: entry.Reference, Pending: tc.pending}))
			}
			done, err := advanceEntry(t.Context(), a, entry, func(context.Context, arguments) error { return tc.cause })
			require.NoError(t, err)
			require.True(t, done)
			_, err = os.Stat(filepath.Join(directory, "blocked.json"))
			require.NoError(t, err)
		})
	}
}

func TestDescriptorTransientReadClassificationAndBackoff(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{
		&loom.Error{StatusCode: 502, Retryable: true, Outcome: loom.Unknown},
		&loom.Error{Retryable: true, Outcome: loom.Unknown},
		&telegram.Error{StatusCode: 502}, telegram.ErrFileUnavailable, context.DeadlineExceeded,
	} {
		require.True(t, transientDescriptorRead(cause), "%v", cause)
	}
	for _, code := range []string{"08006", "53300", "40001", "57P01"} {
		require.True(t, transientCatalogRead(&pgconn.PgError{Code: code}))
	}
	require.False(t, transientCatalogRead(&pgconn.PgError{Code: "28P01"}))
	require.False(t, transientCatalogRead(&pgconn.PgError{Code: "42P01"}))
	now := time.Now()
	first := nextDescriptorRetry(descriptorRetry{}, nil, now)
	second := nextDescriptorRetry(first, nil, now)
	require.Equal(t, 2*time.Second, first.After.Sub(now))
	require.Equal(t, 4*time.Second, second.After.Sub(now))
	for range 100 {
		second = nextDescriptorRetry(second, nil, now)
	}
	require.LessOrEqual(t, second.After.Sub(now), 5*time.Minute)
}

func TestDescriptorCatalogReadPreservesSafeCauseAndOriginalJournal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		code      string
		retryable bool
	}{
		{"08006", true}, {"53300", true}, {"28P01", false}, {"42P01", false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			cause := &pgconn.PgError{Code: tc.code, Message: "postgres://user:SECRET@example.test/catalog"}
			wrapped := &descriptorCatalogReadError{cause}
			require.NotContains(t, wrapped.Error(), "SECRET")
			require.NotContains(t, fmt.Sprint(wrapped), "postgres://")
			require.ErrorIs(t, wrapped, cause)
			require.Equal(t, tc.retryable, transientDescriptorRead(wrapped))
		})
	}
	directory := t.TempDir()
	a := arguments{journal: directory, kind: symbols.Sticker, id: "same", generation: uuid.New()}
	path := filepath.Join(directory, "journal.json")
	original := symboldescribe.State{ProcedureID: uuid.New(), Generation: a.generation,
		Reference: symbols.Reference{Kind: a.kind, ID: a.id}, Phase: "running", RunID: "original-run"}
	require.NoError(t, save(path, original))
	cause := &pgconn.PgError{Code: "08006"}
	got, err := readJournal(a, path, db.SymbolRecord{}, cause)
	require.ErrorIs(t, err, cause)
	require.True(t, transientDescriptorRead(err))
	require.Equal(t, original, got)
	permanent := &pgconn.PgError{Code: "28P01"}
	_, err = readJournal(a, path, db.SymbolRecord{}, permanent)
	require.ErrorIs(t, err, permanent)
	require.False(t, transientDescriptorRead(err))
	// A missing catalog row retains the established eviction behavior.
	_, err = readJournal(a, path, db.SymbolRecord{}, pgx.ErrNoRows)
	require.NoError(t, err)
	// Without an original journal, the lookup cannot prove entry identity.
	_, err = readJournal(a, filepath.Join(directory, "absent.json"), db.SymbolRecord{}, cause)
	require.Error(t, err)
	require.False(t, transientDescriptorRead(err))
}

func TestQueuedGenerationCannotAdoptAnEntryReadmittedBeforeJournalCreation(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "original-generation")
	a := arguments{kind: symbols.Sticker, id: "same-id", journal: dir, generation: uuid.New()}
	entry := db.SymbolRecord{Generation: uuid.New(), Reference: symbols.Reference{Kind: a.kind, ID: a.id}}
	_, err := readJournal(a, filepath.Join(dir, "journal.json"), entry, nil)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBlockedJournalRetainsSafeCauseWithoutRawError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		cause   error
		kind    string
		status  int
		outcome string
	}{
		{"telegram", fmt.Errorf("read sticker set: %w", &telegram.Error{
			StatusCode: 403, Outcome: telegram.Rejected,
		}), "telegram", 403, "rejected"},
		{"loom", &loom.Error{StatusCode: 503, Outcome: loom.Unknown}, "loom", 503, "unknown"},
		{"raw secret", errors.New("GET https://example.test/botSECRET: failure"), "descriptor", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			require.NoError(t, markBlocked(directory, tc.cause))
			raw, err := os.ReadFile(filepath.Join(directory, "blocked.json"))
			require.NoError(t, err)
			require.NotContains(t, string(raw), "SECRET")
			var got blockedFailure
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, blockedFailure{true, tc.kind, tc.status, tc.outcome}, got)
		})
	}
}
