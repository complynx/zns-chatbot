package api

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // The subtests share an isolated, process-wide temp directory.
func TestPassIdentityArchiveStagesOnlyCiphertextAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	for _, env := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(env, dir)
	}
	payload := bytes.Repeat([]byte("synthetic-identity-archive"), 8192)
	failed := errors.New("simulated failure")
	for _, stage := range []string{"success", "build error", "corrupt file", "release denied"} {
		t.Run(stage, func(t *testing.T) {
			released := false
			err := withPassIdentityArchive(func(out io.Writer) error {
				_, writeErr := out.Write(payload)
				require.NoError(t, writeErr)
				files, readErr := os.ReadDir(dir)
				require.NoError(t, readErr)
				require.Len(t, files, 1)
				path := filepath.Join(dir, files[0].Name())
				stored, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.False(t, bytes.Contains(stored, []byte("synthetic-identity-archive")))
				switch stage {
				case "build error":
					return failed
				case "corrupt file":
					require.NoError(t, os.Truncate(path, 0))
				}
				return nil
			}, func(archive io.Reader, size int64) error {
				released = true
				if stage == "release denied" {
					return failed
				}
				require.EqualValues(t, len(payload), size)
				got, readErr := io.ReadAll(archive)
				require.NoError(t, readErr)
				require.Equal(t, payload, got)
				return nil
			})
			if stage == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, stage == "success" || stage == "release denied", released)
			files, readErr := os.ReadDir(dir)
			require.NoError(t, readErr)
			require.Empty(t, files)
		})
	}
}
