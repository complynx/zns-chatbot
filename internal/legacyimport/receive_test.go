package legacyimport_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func validBundle(t *testing.T) ([]byte, legacyimport.BundleResult) {
	t.Helper()
	_, manifest := bundleSnapshot(t)
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	result, err := legacyimport.CreateBundle(manifest, path)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw, result
}

func digest(raw []byte) string { hash := sha256.Sum256(raw); return hex.EncodeToString(hash[:]) }

func TestReceivedBundleIsRevalidatedWithoutExtractingOrImporting(t *testing.T) {
	t.Parallel()
	raw, original := validBundle(t)
	validated, err := legacyimport.ValidateBundle(context.Background(), bytes.NewReader(raw), digest(raw))
	require.NoError(t, err)
	require.Equal(t, *original.Archive, validated.Archive)
	require.Equal(t, original.Inspection, validated.Inspection)
	require.Equal(t, int64(123), validated.SourceBotID)
	public, err := json.Marshal(validated)
	require.NoError(t, err)
	require.NotContains(t, string(public), "PRIVATE_")
	require.NotContains(t, string(public), "source/")
	require.NotContains(t, string(public), "source_bot_id")
	require.False(t, validated.Inspection.ImportPerformed)
	require.False(t, validated.Inspection.CompleteTelegramHistory)
}

type archiveEntry struct {
	header tar.Header
	body   []byte
}

func mutateBundle(t *testing.T, raw []byte, change func([]archiveEntry) []archiveEntry) []byte {
	t.Helper()
	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	reader := tar.NewReader(compressed)
	entries := []archiveEntry{}
	for {
		header, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		require.NoError(t, readErr)
		body, readErr := io.ReadAll(reader)
		require.NoError(t, readErr)
		entries = append(entries, archiveEntry{header: *header, body: body})
	}
	require.NoError(t, compressed.Close())
	var output bytes.Buffer
	zipped := gzip.NewWriter(&output)
	writer := tar.NewWriter(zipped)
	for _, entry := range change(entries) {
		entry.header.Size = int64(len(entry.body))
		require.NoError(t, writer.WriteHeader(&entry.header))
		_, err = writer.Write(entry.body)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, zipped.Close())
	return output.Bytes()
}

func TestReceivingRejectsCorruptionAndTrailingStreamsEvenWithMatchingTransportHash(t *testing.T) {
	t.Parallel()
	raw, _ := validBundle(t)
	corrupt := bytes.Clone(raw)
	corrupt[len(corrupt)-8] ^= 1
	for name, attack := range map[string][]byte{
		"gzip_crc": corrupt, "truncated": raw[:len(raw)-5], "trailing_bytes": append(bytes.Clone(raw), []byte("trailing")...),
		"second_gzip_member": append(bytes.Clone(raw), raw...),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := legacyimport.ValidateBundle(context.Background(), bytes.NewReader(attack), digest(attack))
			require.ErrorIs(t, err, legacyimport.ErrInvalidBundle)
		})
	}
	_, err := legacyimport.ValidateBundle(context.Background(), bytes.NewReader(raw), strings.Repeat("0", 64))
	require.ErrorIs(t, err, legacyimport.ErrInvalidBundle)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = legacyimport.ValidateBundle(ctx, bytes.NewReader(raw), digest(raw))
	require.ErrorIs(t, err, legacyimport.ErrInvalidBundle)
}

func TestReceivingRejectsForgedArchiveStructureAndEmbeddedClaims(t *testing.T) {
	t.Parallel()
	raw, _ := validBundle(t)
	for _, test := range []struct {
		name   string
		change func([]archiveEntry) []archiveEntry
	}{
		{"traversal", func(entries []archiveEntry) []archiveEntry { entries[2].header.Name = "../escape"; return entries }},
		{"symlink", func(entries []archiveEntry) []archiveEntry {
			entries[2].header.Typeflag = tar.TypeSymlink
			entries[2].header.Linkname = "/private"
			entries[2].body = nil
			return entries
		}},
		{"pax_extension", func(entries []archiveEntry) []archiveEntry {
			entries[0].header.Format = tar.FormatPAX
			entries[0].header.PAXRecords = map[string]string{"comment": "untrusted"}
			return entries
		}},
		{"extra_entry", func(entries []archiveEntry) []archiveEntry { return append(entries, entries[0]) }},
		{"oversized_metadata", func(entries []archiveEntry) []archiveEntry {
			entries[0].body = bytes.Repeat([]byte("x"), 65<<10)
			return entries
		}},
		{"changed_original_manifest", func(entries []archiveEntry) []archiveEntry {
			entries[1].body = bytes.ReplaceAll(entries[1].body, []byte(`"bot_id":123`), []byte(`"bot_id":124`))
			return entries
		}},
		{"forged_report", func(entries []archiveEntry) []archiveEntry {
			entries[len(entries)-1].body = []byte(`{"validation_passed":true,"private":"PRIVATE_VALUE"}`)
			return entries
		}},
		{"changed_source", func(entries []archiveEntry) []archiveEntry {
			entries[3].body = bytes.ReplaceAll(entries[3].body, []byte("PRIVATE_MESSAGE"), []byte("CHANGED_MESSAGE"))
			return entries
		}},
		{"forged_owner_and_checksums", func(entries []archiveEntry) []archiveEntry {
			entries[3].body = bytes.ReplaceAll(entries[3].body, []byte(`"user_id":42`), []byte(`"user_id":43`))
			for _, index := range []int{0, 1} {
				var m legacyimport.Manifest
				decodeErr := json.Unmarshal(entries[index].body, &m)
				if decodeErr != nil {
					panic(decodeErr)
				}
				for n := range m.Collections {
					if m.Collections[n].Role == "messages" {
						m.Collections[n].SHA256 = digest(entries[3].body)
					}
				}
				updated, encodeErr := json.Marshal(m)
				if encodeErr != nil {
					panic(encodeErr)
				}
				updated = append(updated, '\n')
				entries[index].body = updated
			}
			return entries
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			attack := mutateBundle(t, raw, test.change)
			_, err := legacyimport.ValidateBundle(context.Background(), bytes.NewReader(attack), digest(attack))
			require.ErrorIs(t, err, legacyimport.ErrInvalidBundle)
		})
	}
}
