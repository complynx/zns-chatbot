package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const watchdogHelperMode = "ZINUSYA_WATCHDOG_HELPER"

func TestWatchdogProcessExitAndDisarm(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("SIGTERM process acceptance targets Linux")
	}
	for _, mode := range []string{"blocked", "disarmed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			executable, err := os.Executable()
			require.NoError(t, err)
			command := exec.CommandContext(ctx, executable, "-test.run=^TestWatchdogHelper$")
			// The helper needs no application credentials or inherited environment.
			command.Env = []string{watchdogHelperMode + "=" + mode, "GORACE=atexit_sleep_ms=0 exitcode=66"}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			stdout, err := command.StdoutPipe()
			require.NoError(t, err)
			stdin, err := command.StdinPipe()
			require.NoError(t, err)
			require.NoError(t, command.Start())
			lines, scanned := watchdogOutput(ctx, stdout)
			waited := make(chan error, 1)
			go func() {
				defer close(waited)
				<-scanned
				waited <- command.Wait()
			}()
			t.Cleanup(func() {
				cancel()
				_ = stdin.Close()
				<-waited
			})
			expectWatchdogLine(ctx, t, lines, "READY")
			started := time.Now()
			require.NoError(t, command.Process.Signal(syscall.SIGTERM))
			if mode == "blocked" {
				expectWatchdogLine(ctx, t, lines, "CLEANUP")
			} else {
				expectWatchdogLine(ctx, t, lines, "DISARMED")
				timer := time.NewTimer(hardShutdownTimeout + time.Second)
				defer timer.Stop()
				select {
				case err = <-waited:
					t.Fatalf("disarmed helper exited before cleanup release: %v", err)
				case <-ctx.Done():
					t.Fatal("helper observation timed out")
				case <-timer.C:
				}
				_, err = io.WriteString(stdin, "release\n")
				require.NoError(t, err)
			}
			err = <-waited
			elapsed := time.Since(started)
			require.NoError(t, ctx.Err(), "parent timeout must not count as a watchdog exit")
			require.Empty(t, stderr.String(), "helper panic/race/test failures must not count as success")
			if mode == "blocked" {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, 1, exit.ExitCode())
				require.GreaterOrEqual(t, elapsed, hardShutdownTimeout)
			} else {
				require.NoError(t, err)
			}
			t.Logf("watchdog mode=%s elapsed=%s", mode, elapsed)
		})
	}
}

func watchdogOutput(ctx context.Context, stdout io.Reader) (<-chan string, <-chan struct{}) {
	lines, scanned := make(chan string, 8), make(chan struct{})
	go func() {
		defer close(scanned)
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	return lines, scanned
}

func expectWatchdogLine(ctx context.Context, t *testing.T, lines <-chan string, expected string) {
	t.Helper()
	select {
	case line, open := <-lines:
		require.True(t, open, "helper closed stdout before %s", expected)
		require.Equal(t, expected, line)
	case <-ctx.Done():
		t.Fatalf("helper did not acknowledge %s: %v", expected, ctx.Err())
	}
}

// TestWatchdogHelper runs only in the isolated subprocess selected above.
func TestWatchdogHelper(t *testing.T) { //nolint:paralleltest // The subprocess owns signals and stdout protocol.
	mode := os.Getenv(watchdogHelperMode)
	if mode == "" || runtime.GOOS != "linux" {
		t.Skip("subprocess helper")
	}
	require.Contains(t, []string{"blocked", "disarmed"}, mode)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	disarm := watchdog(ctx)
	if mode == "blocked" {
		defer disarm()
		// Match run's defer order: cleanup must finish before watchdog disarm.
		defer func() {
			_, _ = fmt.Fprintln(os.Stdout, "CLEANUP")
			for {
				time.Sleep(time.Hour)
			}
		}()
	}
	_, _ = fmt.Fprintln(os.Stdout, "READY")
	<-ctx.Done()
	if mode == "disarmed" {
		disarm()
		_, _ = fmt.Fprintln(os.Stdout, "DISARMED")
		var release [1]byte
		_, err := io.ReadFull(os.Stdin, release[:])
		require.NoError(t, err)
	}
}
