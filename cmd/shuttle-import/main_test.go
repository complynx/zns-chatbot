package main

import (
	"bytes"
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

func TestInspectorRejectsArgumentsWithoutLeakingThem(t *testing.T) {
	t.Parallel()
	var output, diagnostics bytes.Buffer
	require.Equal(t, 2, run([]string{"-database-secret=PRIVATE"}, &output, &diagnostics))
	require.Empty(t, output.String())
	require.NotContains(t, diagnostics.String(), "PRIVATE")
	diagnostics.Reset()
	require.Equal(t, 2, run([]string{"-manifest", strings.Repeat("private", 20)}, &output, &diagnostics))
	require.NotContains(t, diagnostics.String(), strings.Repeat("private", 2))
}

func TestVerifyArchiveRoundTripAndCorruption(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	users := []byte(`{"_id":"u","bot_id":123,"user_id":42,"name":"PRIVATE_NAME"}` + "\n")
	hash := sha256.Sum256(users)
	require.NoError(t, os.WriteFile(filepath.Join(root, "users.ndjson"), users, 0o600))
	manifest := legacyimport.Manifest{Version: 1, SourceInstance: "fixture", BotID: 123,
		DatePolicy: "preserve_source", Collections: []legacyimport.Collection{{Role: "users",
			Name: "users", Path: "users.ndjson", SHA256: hex.EncodeToString(hash[:])}}}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestPath := filepath.Join(root, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))
	archivePath := filepath.Join(root, "archive.tar.gz")
	created, err := legacyimport.CreateBundle(manifestPath, archivePath)
	require.NoError(t, err)
	require.NotNil(t, created.Archive)
	var output, diagnostics bytes.Buffer
	args := []string{"-verify", archivePath, "-sha256", created.Archive.SHA256}
	require.Equal(t, 0, run(args, &output, &diagnostics), diagnostics.String())
	var verified legacyimport.BundleValidation
	require.NoError(t, json.Unmarshal(output.Bytes(), &verified))
	require.Equal(t, *created.Archive, verified.Archive)
	require.Equal(t, created.Inspection, verified.Inspection)
	require.NotContains(t, output.String(), "PRIVATE_NAME")
	require.Empty(t, diagnostics.String())
	output.Reset()
	require.NoError(t, os.WriteFile(archivePath, []byte("PRIVATE_CORRUPTION"), 0o600))
	require.Equal(t, 1, run(args, &output, &diagnostics))
	require.Empty(t, output.String())
	require.NotContains(t, diagnostics.String(), "PRIVATE")
	require.NotContains(t, diagnostics.String(), root)
}

func TestVerificationModesAreExclusive(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-verify", "private"},
		{"-manifest", "private", "-sha256", "private"},
		{"-verify", "private", "-sha256", "private", "-output", "private"},
		{"-verify", "private", "-sha256", "private", "-manifest", "private"},
	} {
		var output, diagnostics bytes.Buffer
		require.Equal(t, 2, run(args, &output, &diagnostics))
		require.Empty(t, output.String())
		require.NotContains(t, diagnostics.String(), "private")
	}
}
