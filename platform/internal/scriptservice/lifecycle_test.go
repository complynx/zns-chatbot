package scriptservice_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
)

const joinTimeout = 5 * time.Second

func TestShutdownJoinsEvaluationChild(t *testing.T) {
	t.Parallel()
	service, pidFile := sleepingService(t)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/evaluate", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		service.ServeHTTP(response, request)
	}()
	child := runningChild(t, pidFile)
	requireUnavailable(t, service)
	ctx, cancel := context.WithTimeout(t.Context(), joinTimeout)
	defer cancel()
	require.NoError(t, service.Shutdown(ctx))
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("evaluation handler did not return after shutdown")
	}
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.NoFileExists(t, child, "shutdown must reap its child before returning success")
	requireUnavailable(t, service)
	require.NoError(t, service.Shutdown(ctx))
}

func TestShutdownJoinsHijackedChild(t *testing.T) {
	t.Parallel()
	service, pidFile := sleepingService(t)
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	connection, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, connection.SetDeadline(time.Now().Add(joinTimeout)))
	_, err = io.WriteString(connection, "CONNECT /execute HTTP/1.1\r\nHost: localhost\r\n\r\n")
	require.NoError(t, err)
	reader := bufio.NewReader(connection)
	status, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "HTTP/1.1 200 Connection Established\r\n", status)
	blank, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "\r\n", blank)
	child := runningChild(t, pidFile)
	ctx, cancel := context.WithTimeout(t.Context(), joinTimeout)
	defer cancel()
	// HTTP shutdown does not own a hijacked connection or its child.
	require.NoError(t, server.Config.Shutdown(ctx))
	require.FileExists(t, child)
	require.NoError(t, service.Shutdown(ctx))
	require.NoFileExists(t, child, "shutdown must reap its hijacked child")
	_, err = reader.ReadByte()
	require.ErrorIs(t, err, io.EOF)
	requireUnavailable(t, service)
}

func TestShutdownDeadlineDoesNotClaimJoinedBody(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	service, err := scriptservice.New(executable)
	require.NoError(t, err)
	input, output := io.Pipe()
	t.Cleanup(func() { _ = input.Close(); _ = output.Close() })
	body := &startedReader{Reader: input, started: make(chan struct{})}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/evaluate", body)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		service.ServeHTTP(httptest.NewRecorder(), request)
	}()
	select {
	case <-body.started:
	case <-time.After(joinTimeout):
		t.Fatal("request body was not admitted")
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	cancel()
	require.ErrorIs(t, service.Shutdown(ctx), context.DeadlineExceeded)
	requireUnavailable(t, service)
	// The HTTP owner closes the stalled body; no child starts for empty input.
	require.NoError(t, output.Close())
	joined, stop := context.WithTimeout(t.Context(), joinTimeout)
	defer stop()
	require.NoError(t, service.Shutdown(joined))
	select {
	case <-finished:
	case <-joined.Done():
		t.Fatal("body handler did not join")
	}
}

func sleepingService(t *testing.T) (*scriptservice.Service, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("child reaping proof uses Linux /proc and /bin/sleep")
	}
	directory := t.TempDir()
	pidFile := filepath.Join(directory, "child.pid")
	executable := filepath.Join(directory, "child")
	quoted := "'" + strings.ReplaceAll(pidFile, "'", "'\"'\"'") + "'"
	program := "#!/bin/sh\nprintf '%s' \"$$\" > " + quoted + "\nexec /bin/sleep 60\n"
	require.NoError(t, os.WriteFile(executable, []byte(program), 0o700))
	service, err := scriptservice.New(executable)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), joinTimeout)
		defer cancel()
		require.NoError(t, service.Shutdown(ctx))
	})
	return service, pidFile
}

func runningChild(t *testing.T, pidFile string) string {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(data))
		return err == nil && pid > 0
	}, joinTimeout, time.Millisecond)
	path := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	require.FileExists(t, path)
	return path
}

func requireUnavailable(t *testing.T, service *scriptservice.Service) {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/evaluate", bytes.NewBufferString(`{}`))
	service.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}

type startedReader struct {
	io.Reader

	started chan struct{}
	once    sync.Once
}

func (r *startedReader) Read(data []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.Reader.Read(data)
}
