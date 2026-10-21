package legacyimport_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

type snapshot struct {
	root     string
	manifest legacyimport.Manifest
	files    map[string][]byte
}

func newSnapshot(t *testing.T, records map[string]string) *snapshot {
	t.Helper()
	s := &snapshot{root: t.TempDir(), files: map[string][]byte{}, manifest: legacyimport.Manifest{
		Version: 1, SourceInstance: "synthetic-legacy", BotID: 123, DatePolicy: "preserve_source",
	}}
	for _, role := range []string{"users", "messages", "passes", "files"} {
		content, present := records[role]
		if !present {
			continue
		}
		raw := []byte(content)
		hash := sha256.Sum256(raw)
		name := role + ".ndjson"
		require.NoError(t, os.WriteFile(filepath.Join(s.root, name), raw, 0o600))
		s.files[name] = raw
		s.manifest.Collections = append(s.manifest.Collections, legacyimport.Collection{
			Role:                role,
			Name:                "legacy_" + role,
			Path:                name,
			SHA256:              hex.EncodeToString(hash[:]),
			SingleBotCollection: role == "messages",
		})
	}
	return s
}

func (s *snapshot) inspect(t *testing.T) (legacyimport.Report, error) {
	t.Helper()
	raw, err := json.Marshal(s.manifest)
	require.NoError(t, err)
	path := filepath.Join(s.root, "snapshot.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return legacyimport.Inspect(path)
}

func TestSnapshotInspectionIsRepeatableReadOnlyAndNotAnImport(t *testing.T) {
	t.Parallel()
	s := newSnapshot(t, map[string]string{
		"users": `{"_id":{"$oid":"1234567890abcdef12345678"},"bot_id":123,"user_id":{"$numberLong":"4503599627370495"},"legal_name":"PRIVATE_NAME","pass_old":{"paid":true,"bank":"PRIVATE_BANK"}}` + "\n",
		"messages": `{"_id":"message-1","user_id":{"$numberLong":"4503599627370495"},"role":"user","content":"PRIVATE_MESSAGE","date":{"$date":{"$numberLong":"0"}}}` + "\n" +
			`{"_id":"message-2","user_id":4503599627370495,"role":"assistant","content":"PRIVATE_REPLY","date":{"$date":"2020-01-01T15:01:02+03:00"}}` + "\n",
		"passes": `{"_id":"pass-1","bot_id":123,"user_id":4503599627370495,"paid":true,"sum":12345}` + "\n",
		"files":  `{"_id":"file-1","bot_id":123,"user_id":4503599627370495,"tg_file_id":"PRIVATE_TG_FILE","status":"completed_simple"}` + "\n",
	})
	first, err := s.inspect(t)
	require.NoError(t, err)
	require.True(t, first.ValidationPassed)
	require.False(t, first.ImportPerformed)
	require.False(t, first.CompleteTelegramHistory)
	require.Equal(t, 2, first.Sources[1].Records)
	second, err := s.inspect(t)
	require.NoError(t, err)
	require.Equal(t, first, second)
	for name, original := range s.files {
		raw, readErr := os.ReadFile(filepath.Join(s.root, name))
		require.NoError(t, readErr)
		require.Equal(t, original, raw)
	}
	raw, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "PRIVATE_")
	require.NotContains(t, string(raw), "4503599627370495")
}

func TestSnapshotRejectsUnsafeRecordsWithoutDisclosingTheirContents(t *testing.T) {
	t.Parallel()
	const user = `{"_id":"u","bot_id":123,"user_id":42}`
	for _, test := range []struct{ name, role, raw, code string }{
		{"mixed_bot", "users", `{"_id":"u","bot_id":124,"user_id":42}`, "bot_scope_mismatch"},
		{"orphan", "passes", `{"_id":"p","bot_id":123,"user_id":43}`, "orphan_owner"},
		{"duplicate", "users", user + "\n" + user, "duplicate_source_id"},
		{"conflict", "users", user + "\n" + `{"_id":"u","bot_id":123,"user_id":43}`, "conflicting_source_id"},
		{"identity_conflict", "users", user + "\n" + `{"_id":"u2","bot_id":123,"user_id":42}`, "conflicting_owner_id"},
		{"duplicate_json", "users", `{"_id":"u","bot_id":124,"bot_id":123,"user_id":42,"name":"PRIVATE_VALUE"}`, "invalid_record_json"},
		{"fractional_owner", "users", `{"_id":"u","bot_id":123,"user_id":42.5}`, "invalid_owner_id"},
		{"missing_scope", "passes", `{"_id":"p","user_id":42}`, "missing_bot_id"},
		{"naive_date", "messages", `{"_id":"m","user_id":42,"date":{"$date":"2020-01-01T12:00:00"}}`, "unsupported_source_date"},
		{"malformed", "users", `{"_id":`, "invalid_record_json"},
		{"invalid_utf8", "users", "{\"_id\":\"u\",\"bot_id\":123,\"user_id\":42,\"name\":\"\xff\"}", "invalid_record_json"},
		{"nested_json", "users", `{"_id":"u","bot_id":123,"user_id":42,"x":{"secret":1,"secret":2}}`, "invalid_record_json"},
		{"deep_json", "users", `{"_id":"u","bot_id":123,"user_id":42,"x":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}", "invalid_record_json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			records := map[string]string{"users": user}
			records[test.role] = test.raw
			s := newSnapshot(t, records)
			r, err := s.inspect(t)
			require.NoError(t, err)
			require.False(t, r.ValidationPassed)
			require.Contains(
				t,
				r.Issues,
				legacyimport.Issue{Role: test.role, Line: strings.Count(test.raw, "\n") + 1, Code: test.code},
			)
			output, err := json.Marshal(r)
			require.NoError(t, err)
			require.NotContains(t, string(output), "PRIVATE_VALUE")
		})
	}
}

func TestSnapshotRequiresChecksumsAndExplicitAssistantScope(t *testing.T) {
	t.Parallel()
	t.Run("checksum", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(t, map[string]string{"users": `{"_id":"u","bot_id":123,"user_id":42}`})
		s.manifest.Collections[0].SHA256 = strings.Repeat("0", 64)
		r, err := s.inspect(t)
		require.NoError(t, err)
		require.False(t, r.ValidationPassed)
		require.Equal(t, []legacyimport.Issue{{Role: "users", Code: "checksum_mismatch"}}, r.Issues)
	})
	t.Run("scope", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(t, map[string]string{"users": "", "messages": ""})
		s.manifest.Collections[1].SingleBotCollection = false
		_, err := s.inspect(t)
		require.ErrorIs(t, err, legacyimport.ErrManifest)
	})
	t.Run("escape", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(t, map[string]string{"users": ""})
		s.manifest.Collections[0].Path = "../private.ndjson"
		_, err := s.inspect(t)
		require.ErrorIs(t, err, legacyimport.ErrManifest)
	})
	t.Run("line_limit", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(t, map[string]string{"users": strings.Repeat("x", 8<<20)})
		r, err := s.inspect(t)
		require.NoError(t, err)
		require.False(t, r.ValidationPassed)
		require.Equal(t, "source_read_or_line_limit", r.Issues[0].Code)
		require.Positive(t, r.Sources[0].Bytes, "Failed scans still report and charge bytes read")
	})
	t.Run("omitted_roles", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(t, map[string]string{"users": `{"_id":"u","bot_id":123,"user_id":42}`})
		r, err := s.inspect(t)
		require.NoError(t, err)
		require.True(t, r.ValidationPassed)
		require.False(t, r.Sources[1].Present)
		require.False(t, r.CompleteTelegramHistory)
	})
}

func TestSnapshotRejectsManifestAliasesAndSameFileUnderTwoRoles(t *testing.T) {
	t.Parallel()
	t.Run("case_alias", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(t, map[string]string{"users": ""})
		raw, err := json.Marshal(s.manifest)
		require.NoError(t, err)
		raw = []byte(strings.Replace(string(raw), `"bot_id":123`, `"bot_id":124,"BOT_ID":123`, 1))
		path := filepath.Join(s.root, "snapshot.json")
		require.NoError(t, os.WriteFile(path, raw, 0o600))
		_, err = legacyimport.Inspect(path)
		require.ErrorIs(t, err, legacyimport.ErrManifest)
	})
	t.Run("hardlink_alias", func(t *testing.T) {
		t.Parallel()
		s := newSnapshot(
			t,
			map[string]string{"users": `{"_id":"u","bot_id":123,"user_id":42,"date":{"$date":"2020-01-01T00:00:00Z"}}`},
		)
		require.NoError(t, os.Link(filepath.Join(s.root, "users.ndjson"), filepath.Join(s.root, "messages.ndjson")))
		s.manifest.Collections = append(s.manifest.Collections, legacyimport.Collection{
			Role:                "messages",
			Name:                "legacy_messages",
			Path:                "messages.ndjson",
			SHA256:              s.manifest.Collections[0].SHA256,
			SingleBotCollection: true,
		})
		r, err := s.inspect(t)
		require.NoError(t, err)
		require.False(t, r.ValidationPassed)
		require.Contains(t, r.Issues, legacyimport.Issue{Role: "messages", Code: "duplicate_source_file"})
	})
}

func TestSnapshotAcceptsExactDoubleIdentitiesWithoutChangingSource(t *testing.T) {
	t.Parallel()
	for _, number := range []string{`42.0`, `4.2e1`, `{"$numberDouble":"42.0"}`, `{"$numberDouble":"4.2E+1"}`, `4503599627370495.0`} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			s := newSnapshot(t, map[string]string{
				"users":    `{"_id":"u","bot_id":{"$numberDouble":"123.0"},"user_id":` + number + `}`,
				"passes":   `{"_id":"p","bot_id":123.0,"user_id":` + number + `}`,
				"messages": `{"_id":"m","user_id":` + number + `,"role":"user","date":{"$date":{"$numberLong":"0"}}}`,
			})
			r, err := s.inspect(t)
			require.NoError(t, err)
			require.True(t, r.ValidationPassed, "%+v", r.Issues)
			for name, original := range s.files {
				raw, readErr := os.ReadFile(filepath.Join(s.root, name))
				require.NoError(t, readErr)
				require.Equal(t, original, raw)
			}
		})
	}
}

func TestSnapshotRejectsInexactAndUnsafeDoubleIdentities(t *testing.T) {
	t.Parallel()
	for _, number := range []string{`42.5`, `4503599627370495.1`, `4503599627370496.0`, `{"$numberDouble":"NaN"}`, `{"$numberDouble":"Infinity"}`, `{"$numberDouble":"42.0000000000000001"}`, `{"$numberDouble":"1e-999999999"}`, `{"$numberDouble":"1/1"}`, `{"$numberDouble":"42","extra":true}`} {
		t.Run(number, func(t *testing.T) {
			t.Parallel()
			s := newSnapshot(t, map[string]string{"users": `{"_id":"u","bot_id":123,"user_id":` + number + `}`})
			r, err := s.inspect(t)
			require.NoError(t, err)
			require.False(t, r.ValidationPassed)
			require.Contains(t, r.Issues, legacyimport.Issue{Role: "users", Line: 1, Code: "invalid_owner_id"})
		})
	}
}
