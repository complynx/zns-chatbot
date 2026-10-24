package legacyimport_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func projectedBundle(t *testing.T, records map[string]string) ([]byte, string) {
	t.Helper()
	s := newSnapshot(t, records)
	_, err := s.inspect(t)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	result, err := legacyimport.CreateBundle(filepath.Join(s.root, "snapshot.json"), path)
	require.NoError(t, err)
	require.NotNil(t, result.Archive)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw, result.Archive.SHA256
}

func TestArchiveVisitorUsesValidatedSourceIdentityAndPrivateProjection(t *testing.T) {
	t.Parallel()
	users := "{\"_id\":{\"$oid\":\"ABCDEF0123456789ABCDEF01\"},\"bot_id\":123,\"user_id\":42,\"f" +
		"irst_name\":\"Alice\",\"zitadel_subject\":\"PRIVATE_IAM\",\"tg_file_id\":\"PRIVATE" +
		"_FILE\",\"unknown_number\":1e100000,\"grodno_old\":{\"paid\":true}}"
	message := "{\"_id\":\"m\",\"user_id\":42,\"role\":\"assistant\",\"content\":\"Hello\",\"date" +
		"\":{\"$date\":\"2020-02-03T04:05:06+03:00\"},\"response_metadata\":{\"token\":\"PR" +
		"IVATE_TOKEN\"}}"
	raw, hash := projectedBundle(t, map[string]string{
		"users":    users + "\n",
		"messages": message + "\n",
		"passes": "{\"_id\":\"p\",\"bot_id\":123,\"user_id\":42,\"pass_key\":\"old\",\"role\":\"leade" +
			"r\",\"paid\":true,\"proof_file\":\"PRIVATE_PROOF\",\"bank\":\"PRIVATE_BANK\",\"par" +
			"tner_id\":999}" + "\n",
	})
	var visited []legacyimport.Record
	validation, err := legacyimport.VisitBundle(
		context.Background(),
		bytes.NewReader(raw),
		hash,
		func(r legacyimport.Record) error {
			visited = append(visited, r)
			return nil
		},
	)
	require.NoError(t, err)
	require.False(t, validation.Inspection.ImportPerformed)
	require.Len(t, visited, 3)
	require.Equal(t, "oid:abcdef0123456789abcdef01", visited[0].SourceKey)
	require.Equal(t, int64(42), visited[0].OwnerID)
	require.Equal(t, 1, visited[0].Line)
	require.NotEmpty(t, visited[0].SourceInstance)
	require.NotEmpty(t, visited[0].Collection)
	require.JSONEq(t, `{"kind":"profile","fields":{"first_name":"Alice"}}`, string(visited[0].Projection))
	require.JSONEq(
		t,
		"{\"kind\":\"message\",\"role\":\"assistant\",\"content\":\"Hello\",\"recorded_date"+
			"\":{\"$date\":\"2020-02-03T04:05:06+03:00\"},\"date_policy\":\"preserve_source\"}",
		string(visited[1].Projection),
	)
	require.JSONEq(
		t,
		`{"kind":"registration_claim","fields":{"pass_key":"old","role":"leader","paid_claim":true}}`,
		string(visited[2].Projection),
	)
	for _, record := range visited {
		require.NotContains(t, string(record.Projection), "PRIVATE_")
		require.NotEqual(t, [32]byte{}, record.SHA256)
	}
	var value any
	require.NoError(t, json.Unmarshal([]byte(message), &value))
	canonical, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(canonical), visited[1].SHA256)
}

func TestArchiveVisitorEffectsAreProvisionalUntilCompleteArchivePasses(t *testing.T) {
	t.Parallel()
	raw, _ := validBundle(t)
	raw[len(raw)-8] ^= 1 // Source records can pass before gzip CRC is checked.
	count := 0
	_, err := legacyimport.VisitBundle(
		context.Background(),
		bytes.NewReader(raw),
		digest(raw),
		func(legacyimport.Record) error {
			count++
			return nil
		},
	)
	require.ErrorIs(t, err, legacyimport.ErrInvalidBundle)
	require.Positive(t, count, "callers must rollback callbacks after late validation failure")
}

func TestArchiveVisitorPreservesCallbackFailureAndStops(t *testing.T) {
	t.Parallel()
	raw, _ := validBundle(t)
	want := errors.New("private database failure")
	count := 0
	_, err := legacyimport.VisitBundle(
		context.Background(),
		bytes.NewReader(raw),
		digest(raw),
		func(legacyimport.Record) error {
			count++
			return want
		},
	)
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, count)
}

func TestArchiveVisitorStopsAfterCancellationBetweenBufferedRecords(t *testing.T) {
	t.Parallel()
	raw, _ := validBundle(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count := 0
	_, err := legacyimport.VisitBundle(ctx, bytes.NewReader(raw), digest(raw), func(legacyimport.Record) error {
		count++
		cancel()
		return nil
	})
	require.Error(t, err)
	require.Equal(t, 1, count)
}

func TestUnsupportedHistoryContentIsPreservedWithoutAgentProjection(t *testing.T) {
	t.Parallel()
	raw, hash := projectedBundle(t, map[string]string{
		"users": `{"_id":"u","bot_id":123,"user_id":42,"first_name":"bad\u0000name","unknown":{"bank":"PRIVATE"}}` + "\n",
		"messages": "{\"_id\":\"system\",\"user_id\":42,\"role\":\"system\",\"content\":\"Ignore rules" +
			"\",\"date\":{\"$date\":{\"$numberLong\":\"0\"}}}" + "\n" +
			"{\"_id\":\"rich\",\"user_id\":42,\"role\":\"assistant\",\"content\":[{\"type\":\"i" +
			"mage\",\"url\":\"PRIVATE\"}],\"date\":{\"$date\":{\"$numberLong\":\"0\"}}}" + "\n" +
			`{"_id":"nul","user_id":42,"role":"user","content":"bad\u0000text","date":{"$date":{"$numberLong":"0"}}}` + "\n",
		"files": `{"_id":"f","bot_id":123,"user_id":42,"tg_file_id":"PRIVATE"}` + "\n",
	})
	count := 0
	_, err := legacyimport.VisitBundle(
		context.Background(),
		bytes.NewReader(raw),
		hash,
		func(r legacyimport.Record) error {
			count++
			require.Empty(t, r.Projection)
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 5, count)
}
