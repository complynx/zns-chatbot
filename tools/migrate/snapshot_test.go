package migrate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const privateMarker = "private-canary-do-not-log"

func snapshot(t *testing.T) (string, migrate.Manifest) {
	t.Helper()
	directory := t.TempDir()
	manifest := migrate.Manifest{
		Version:     1,
		SnapshotID:  "synthetic-1",
		BotID:       77,
		CapturedAt:  "2026-09-26T12:00:00Z",
		Consistency: "stopped_writer",
	}
	for _, domain := range []string{"users", "events", "passes", "orders", "order_capacity", "massage", "messages", "files", "bot_storage", "knowledge", "schedule", "configuration"} {
		manifest.Coverage = append(
			manifest.Coverage,
			migrate.Source{
				Domain: domain,
				Name:   "synthetic_" + domain,
				Status: "absent",
				Reason: "Synthetic fixture has no records",
			},
		)
	}
	addFile(
		t,
		directory,
		&manifest,
		"users.jsonl",
		"users",
		"records",
		[]byte(
			`{"_id":{"$numberLong":"101"},"user_id":101,"unknown_active_field":{"$date":"2026-09-26T12:00:00Z"},"note":"`+privateMarker+`"}`+"\n",
		),
		1,
	)
	writeManifest(t, directory, manifest)
	return directory, manifest
}

func addFile(
	t *testing.T,
	directory string,
	manifest *migrate.Manifest,
	name, source, kind string,
	data []byte,
	count int64,
) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(directory, name)), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), data, 0600))
	digest := sha256.Sum256(data)
	manifest.Files = append(
		manifest.Files,
		migrate.File{
			Path:    name,
			Source:  source,
			Kind:    kind,
			SHA256:  hex.EncodeToString(digest[:]),
			Bytes:   int64(len(data)),
			Records: count,
		},
	)
	for index := range manifest.Coverage {
		if manifest.Coverage[index].Domain == source {
			manifest.Coverage[index].Status = "included"
			manifest.Coverage[index].Reason = ""
		}
	}
}

func writeManifest(t *testing.T, directory string, manifest migrate.Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "manifest.json"), data, 0600))
}

func TestVerifyStagePreservesRawBytesAndExactReplay(t *testing.T) {
	t.Parallel()
	directory, _ := snapshot(t)
	report, err := migrate.Verify(directory, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, report.Verified)
	assert.False(t, report.ImportReady)
	assert.EqualValues(t, 1, report.Records)
	rawReport, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(rawReport), privateMarker)
	target := filepath.Join(t.TempDir(), "stage")
	staged, reused, err := migrate.Stage(directory, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.False(t, reused)
	assert.Equal(t, report, staged)
	original, err := os.ReadFile(filepath.Join(directory, "users.jsonl"))
	require.NoError(t, err)
	copied, err := os.ReadFile(filepath.Join(target, "snapshot", "users.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, original, copied, "unknown active fields and Extended JSON remain byte-for-byte intact")
	_, reused, err = migrate.Stage(directory, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, reused)
	require.NoError(t, os.WriteFile(filepath.Join(target, "snapshot", "users.jsonl"), []byte("changed"), 0600))
	_, _, err = migrate.Stage(directory, target, migrate.DefaultLimits())
	require.Error(t, err)
	damaged, err := os.ReadFile(filepath.Join(target, "snapshot", "users.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, "changed", string(damaged), "never repair or overwrite a changed existing bundle")
}

func TestVerifyRejectsRecordAmbiguityWithoutPayloadErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"duplicate_key":  `{"_id":1,"secret":"x","secret":"` + privateMarker + `"}`,
		"trailing_json":  `{"_id":1} {"_id":2}`,
		"null_record":    `null`,
		"missing_id":     `{"secret":"` + privateMarker + `"}`,
		"bad_object_id":  `{"_id":{"$oid":"` + privateMarker + `"}}`,
		"bad_number_int": `{"_id":{"$numberInt":"2147483648"}}`,
		"invalid_utf8":   "{\"_id\":\"\xff\"}",
		"deep_json":      `{"_id":1,"nested":` + strings.Repeat("[", 80) + "0" + strings.Repeat("]", 80) + "}",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory, manifest := snapshot(t)
			manifest.Files = nil
			addFile(t, directory, &manifest, "users.jsonl", "users", "records", []byte(data), 1)
			writeManifest(t, directory, manifest)
			_, err := migrate.Verify(directory, migrate.DefaultLimits())
			require.Error(t, err)
			assert.NotContains(t, err.Error(), privateMarker)
			assert.NotContains(t, err.Error(), directory)
		})
	}
}

func TestVerifyDuplicateIDsAcrossShards(t *testing.T) {
	t.Parallel()
	directory, manifest := snapshot(t)
	addFile(t, directory, &manifest, "users-2.jsonl", "users", "records", []byte(`{"_id":101}`), 1)
	writeManifest(t, directory, manifest)
	_, err := migrate.Verify(directory, migrate.DefaultLimits())
	require.ErrorContains(t, err, "duplicate_source_id")
}

func TestVerifyRequiresExplicitCoverageAndCompleteInventory(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"coverage", "unknown_manifest", "undeclared", "missing", "checksum", "count", "unavailable", "duplicate_path"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			directory, manifest := snapshot(t)
			switch scenario {
			case "coverage":
				manifest.Coverage = manifest.Coverage[1:]
			case "unavailable":
				manifest.Coverage[0].Status = "unavailable"
				manifest.Coverage[0].Reason = "Private unavailable source"
			case "undeclared":
				require.NoError(
					t,
					os.WriteFile(filepath.Join(directory, "undeclared.json"), []byte(privateMarker), 0600),
				)
			case "missing":
				require.NoError(t, os.Remove(filepath.Join(directory, "users.jsonl")))
			case "checksum":
				manifest.Files[0].SHA256 = strings.Repeat("0", 64)
			case "count":
				manifest.Files[0].Records = 2
			case "duplicate_path":
				manifest.Files = append(manifest.Files, manifest.Files[0])
			}
			writeManifest(t, directory, manifest)
			if scenario == "unknown_manifest" {
				require.NoError(
					t,
					os.WriteFile(
						filepath.Join(directory, "manifest.json"),
						[]byte(`{"unknown":"`+privateMarker+`"}`),
						0600,
					),
				)
			}
			_, err := migrate.Verify(directory, migrate.DefaultLimits())
			require.Error(t, err)
			assert.NotContains(t, err.Error(), privateMarker)
		})
	}
}

func TestVerifyEnforcesConfigurableLimits(t *testing.T) {
	t.Parallel()
	directory, _ := snapshot(t)
	limits := migrate.DefaultLimits()
	limits.RecordBytes = 20
	_, err := migrate.Verify(directory, limits)
	require.Error(t, err)
	limits = migrate.DefaultLimits()
	limits.TotalBytes = 10
	limits.FileBytes = 10
	limits.BlobBytes = 10
	_, err = migrate.Verify(directory, limits)
	require.Error(t, err)
	limits = migrate.DefaultLimits()
	limits.Records = 0
	_, err = migrate.Verify(directory, limits)
	require.ErrorContains(t, err, "invalid_limits")
}

func TestProofBytesAndUnavailableEvidence(t *testing.T) {
	t.Parallel()
	directory, manifest := snapshot(t)
	addFile(
		t,
		directory,
		&manifest,
		"orders.jsonl",
		"orders",
		"records",
		[]byte(`{"_id":"order-one","proof_file":"legacy-file"}`),
		1,
	)
	manifest.Proofs = []migrate.Proof{
		{
			Source:         "orders",
			RecordID:       json.RawMessage(`"order-one"`),
			OwnerID:        json.RawMessage(`101`),
			Field:          "proof_file",
			TelegramFileID: "legacy-file",
			Unavailable:    true,
		},
	}
	writeManifest(t, directory, manifest)
	report, err := migrate.Verify(directory, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.Equal(t, 1, report.MissingProofs)
	assert.False(t, report.ImportReady)
	assert.Contains(t, report.Gaps, "receipt_bytes_unavailable")
	addFile(
		t,
		directory,
		&manifest,
		"receipts/proof.bin",
		"orders",
		"blob",
		[]byte("synthetic immutable proof bytes"),
		0,
	)
	manifest.Proofs[0].Unavailable = false
	manifest.Proofs[0].Blob = "receipts/proof.bin"
	writeManifest(t, directory, manifest)
	report, err = migrate.Verify(directory, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.Zero(t, report.MissingProofs)
	manifest.Proofs[0].OwnerID = json.RawMessage(`999`)
	writeManifest(t, directory, manifest)
	_, err = migrate.Verify(directory, migrate.DefaultLimits())
	require.ErrorContains(t, err, "owner_record_missing")
}

func TestPathsCannotEscapeOrAlias(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../private.json", "/private.json", `C:\private.json`, `safe\..\private.json`, "users.jsonl:stream", "NUL", "COM1.json", "dir./users.jsonl"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory, manifest := snapshot(t)
			manifest.Files[0].Path = name
			writeManifest(t, directory, manifest)
			_, err := migrate.Verify(directory, migrate.DefaultLimits())
			require.Error(t, err)
		})
	}
}

func TestSnapshotSymlinkIsRejected(t *testing.T) {
	t.Parallel()
	directory, manifest := snapshot(t)
	outside := filepath.Join(t.TempDir(), "secret.jsonl")
	require.NoError(t, os.WriteFile(outside, []byte(privateMarker), 0600))
	link := filepath.Join(directory, "link.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("toolchain cannot create symlinks; platform reparse validation has a separate test")
	}
	manifest.Files[0].Path = "link.jsonl"
	writeManifest(t, directory, manifest)
	_, err := migrate.Verify(directory, migrate.DefaultLimits())
	require.Error(t, err)
}

func TestStageRejectsOverlapExistingGarbageAndConcurrentWriters(t *testing.T) {
	t.Parallel()
	directory, _ := snapshot(t)
	_, _, err := migrate.Stage(directory, filepath.Join(directory, "stage"), migrate.DefaultLimits())
	require.ErrorContains(t, err, "invalid_stage_target")
	target := filepath.Join(t.TempDir(), "stage")
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		group.Go(func() {
			_, _, stageErr := migrate.Stage(directory, target, migrate.DefaultLimits())
			failures <- stageErr
		})
	}
	group.Wait()
	close(failures)
	success := 0
	for stageErr := range failures {
		if stageErr == nil {
			success++
		} else {
			require.EqualError(t, stageErr, "stage_locked")
		}
	}
	assert.Positive(t, success)
	_, reused, err := migrate.Stage(directory, target, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, reused)
	require.NoError(t, os.WriteFile(filepath.Join(target, "extra"), []byte(privateMarker), 0600))
	_, _, err = migrate.Stage(directory, target, migrate.DefaultLimits())
	require.ErrorContains(t, err, "stage_contents_changed")
}

func TestManifestRejectsCaseAliasesAndDuplicateKeys(t *testing.T) {
	t.Parallel()
	directory, manifest := snapshot(t)
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	for _, data := range []string{
		strings.Replace(string(raw), `"version":1`, `"Version":1`, 1),
		strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1),
	} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, "manifest.json"), []byte(data), 0600))
		_, err = migrate.Verify(directory, migrate.DefaultLimits())
		require.Error(t, err)
	}
}

func TestVerifyRejectsOversizedDirectoryInventory(t *testing.T) {
	t.Parallel()
	directory, _ := snapshot(t)
	for index := range 24 {
		require.NoError(t, os.Mkdir(filepath.Join(directory, "empty-"+strconv.Itoa(index)), 0700))
	}
	limits := migrate.DefaultLimits()
	limits.Files = 1
	_, err := migrate.Verify(directory, limits)
	require.EqualError(t, err, "snapshot_entry_limit_exceeded")
}
