package migrate_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func TestFIFOPathsFailWithoutBlocking(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"manifest", "stage_receipt"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			directory, _ := snapshot(t)
			target := ""
			fifo := filepath.Join(directory, "manifest.json")
			if kind == "stage_receipt" {
				target = filepath.Join(t.TempDir(), "stage")
				_, _, err := migrate.Stage(directory, target, migrate.DefaultLimits())
				require.NoError(t, err)
				fifo = filepath.Join(target, "stage.json")
			}
			require.NoError(t, os.Remove(fifo))
			require.NoError(t, syscall.Mkfifo(fifo, 0600))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			process := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFIFOOpenHelper$")
			process.Env = append(os.Environ(), "ZNS_MIGRATE_FIFO_SOURCE="+directory, "ZNS_MIGRATE_FIFO_TARGET="+target)
			output, err := process.CombinedOutput()
			require.NoError(t, ctx.Err(), "opening a static FIFO must not wait for a writer")
			require.NoError(t, err, string(output))
		})
	}
}

func TestFIFOOpenHelper(t *testing.T) {
	t.Parallel()
	source := os.Getenv("ZNS_MIGRATE_FIFO_SOURCE")
	if source == "" {
		return
	}
	target := os.Getenv("ZNS_MIGRATE_FIFO_TARGET")
	var err error
	if target == "" {
		_, err = migrate.Verify(source, migrate.DefaultLimits())
	} else {
		_, _, err = migrate.Stage(source, target, migrate.DefaultLimits())
	}
	require.EqualError(t, err, "not_regular_file")
}
