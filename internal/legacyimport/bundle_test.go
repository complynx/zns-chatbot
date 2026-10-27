package legacyimport_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func bundleSnapshot(t *testing.T) (*snapshot, string) {
	t.Helper()
	s := newSnapshot(t, map[string]string{
		"users": `{"_id":"u","bot_id":123,"user_id":42,"name":"PRIVATE_NAME",` +
			`"pass_old":{"paid":true}}` + "\r\n",
		"messages": `{"_id":"m","user_id":42,"content":"PRIVATE_MESSAGE",` +
			`"date":{"$date":"2020-01-01T00:00:00Z"}}` + "\n",
		"passes": `{"_id":"p","bot_id":123,"user_id":42,"paid":true}` + "\n",
		"files":  `{"_id":"f","bot_id":123,"user_id":42,"tg_file_id":"PRIVATE_FILE"}` + "\n",
	})
	_, err := s.inspect(t)
	require.NoError(t, err)
	return s, filepath.Join(s.root, "snapshot.json")
}

func readBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, compressed.Close()) })
	require.Empty(t, compressed.Name)
	require.True(t, compressed.ModTime.IsZero())
	reader := tar.NewReader(compressed)
	entries := map[string][]byte{}
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		require.Equal(t, byte(tar.TypeReg), header.Typeflag)
		require.Equal(t, int64(0o600), header.Mode)
		require.Zero(t, header.Uid)
		require.Zero(t, header.Gid)
		require.Equal(t, int64(0), header.ModTime.Unix())
		require.NotContains(t, entries, header.Name)
		body, readErr := io.ReadAll(reader)
		require.NoError(t, readErr)
		entries[header.Name] = body
	}
	return entries
}

func TestBundlePreservesExactSourcesAndCanBeInspectedAfterExtraction(t *testing.T) {
	t.Parallel()
	s, manifest := bundleSnapshot(t)
	original, err := os.ReadFile(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(s.root, "unrelated-private.txt"), []byte("NOT_IN_BUNDLE"), 0o600))
	destination := filepath.Join(t.TempDir(), "history.tar.gz")
	result, err := legacyimport.CreateBundle(manifest, destination)
	require.NoError(t, err)
	require.True(t, result.Inspection.ValidationPassed)
	require.False(t, result.Inspection.ImportPerformed)
	require.False(t, result.Inspection.CompleteTelegramHistory)
	require.NotNil(t, result.Archive)
	raw, err := os.ReadFile(destination)
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	require.Equal(t, hex.EncodeToString(digest[:]), result.Archive.SHA256)
	require.Equal(t, int64(len(raw)), result.Archive.SizeBytes)
	entries := readBundle(t, destination)
	require.Len(t, entries, 7)
	require.Equal(t, original, entries["original-manifest.json"])
	require.NotContains(t, entries, "unrelated-private.txt")
	for name, source := range s.files {
		require.Equal(t, source, entries["source/"+name])
	}
	var inspected legacyimport.Report
	require.NoError(t, json.Unmarshal(entries["inspection-report.json"], &inspected))
	require.Equal(t, result.Inspection, inspected)
	extracted := t.TempDir()
	for name, body := range entries {
		require.True(t, filepath.IsLocal(name))
		path := filepath.Join(extracted, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, body, 0o600))
	}
	report, err := legacyimport.Inspect(filepath.Join(extracted, "bundle-manifest.json"))
	require.NoError(t, err)
	require.Equal(t, result.Inspection, report)
	public, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(public), "PRIVATE_")
	require.NotContains(t, string(public), destination)
	remaining, err := os.ReadDir(filepath.Dir(destination))
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}

func TestBundleIsByteRepeatableForTheSameSnapshotAcrossDirectories(t *testing.T) {
	t.Parallel()
	_, firstManifest := bundleSnapshot(t)
	_, secondManifest := bundleSnapshot(t)
	first, second := filepath.Join(t.TempDir(), "first.tar.gz"), filepath.Join(t.TempDir(), "second.tar.gz")
	a, err := legacyimport.CreateBundle(firstManifest, first)
	require.NoError(t, err)
	b, err := legacyimport.CreateBundle(secondManifest, second)
	require.NoError(t, err)
	require.Equal(t, a, b)
	x, err := os.ReadFile(first)
	require.NoError(t, err)
	y, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, x, y)
}

func TestInvalidSnapshotAndExistingDestinationLeaveNoPartialArchive(t *testing.T) {
	t.Parallel()
	t.Run("source_changed", func(t *testing.T) {
		t.Parallel()
		s, manifest := bundleSnapshot(t)
		require.NoError(
			t,
			os.WriteFile(
				filepath.Join(s.root, "users.ndjson"),
				[]byte(`{"_id":"changed","bot_id":123,"user_id":42}`),
				0o600,
			),
		)
		outputDir := t.TempDir()
		destination := filepath.Join(outputDir, "history.tar.gz")
		result, err := legacyimport.CreateBundle(manifest, destination)
		require.NoError(t, err)
		require.False(t, result.Inspection.ValidationPassed)
		require.Nil(t, result.Archive)
		entries, err := os.ReadDir(outputDir)
		require.NoError(t, err)
		require.Empty(t, entries)
	})
	t.Run("existing_destination", func(t *testing.T) {
		t.Parallel()
		_, manifest := bundleSnapshot(t)
		outputDir := t.TempDir()
		destination := filepath.Join(outputDir, "existing.tar.gz")
		require.NoError(t, os.WriteFile(destination, []byte("KEEP_EXISTING"), 0o600))
		result, err := legacyimport.CreateBundle(manifest, destination)
		require.ErrorIs(t, err, legacyimport.ErrArchive)
		require.Nil(t, result.Archive)
		raw, err := os.ReadFile(destination)
		require.NoError(t, err)
		require.Equal(t, "KEEP_EXISTING", string(raw))
		entries, err := os.ReadDir(outputDir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
	})
}

func TestConcurrentBundlePublicationHasExactlyOneWinner(t *testing.T) {
	t.Parallel()
	_, manifest := bundleSnapshot(t)
	outputDir := t.TempDir()
	destination := filepath.Join(outputDir, "history.tar.gz")
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		group.Go(func() { _, err := legacyimport.CreateBundle(manifest, destination); errors <- err })
	}
	group.Wait()
	close(errors)
	winners := 0
	for err := range errors {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, legacyimport.ErrArchive)
		}
	}
	require.Equal(t, 1, winners)
	require.Len(t, readBundle(t, destination), 7)
	entries, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestBundleLimitFailureKeepsItsValidationReportWithoutPublication(t *testing.T) {
	t.Parallel()
	s := newSnapshot(t, map[string]string{
		"users":    strings.Repeat("x", 9<<20),
		"messages": `{"_id":"m","user_id":42,"date":{"$date":"2020-01-01T00:00:00Z"}}`,
	})
	_, err := s.inspect(t)
	require.NoError(t, err)
	outputDir := t.TempDir()
	result, err := legacyimport.CreateBundle(
		filepath.Join(s.root, "snapshot.json"),
		filepath.Join(outputDir, "invalid.tar.gz"),
	)
	require.NoError(t, err)
	require.False(t, result.Inspection.ValidationPassed)
	require.Nil(t, result.Archive)
	require.Contains(
		t,
		result.Inspection.Issues,
		legacyimport.Issue{Role: "users", Line: 1, Code: "source_read_or_line_limit"},
	)
	remaining, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	require.Empty(t, remaining)
}
