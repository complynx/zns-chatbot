// Package scriptservice supervises one-request JavaScript child processes.
// Deploy this service on a Unix socket in a networkless, memory-limited container.
package scriptservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

type Service struct {
	executable string
	sessions   lifecycle
}

func New(executable string) (*Service, error) {
	if !filepath.IsAbs(executable) {
		return nil, errors.New("script executable must be absolute")
	}
	if _, err := exec.LookPath(executable); err != nil {
		return nil, errors.New("script executable unavailable")
	}
	return &Service{executable: executable}, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rpc := r.Method == http.MethodConnect && r.URL.Path == "/execute"
	if !rpc && (r.Method != http.MethodPost || r.URL.Path != "/evaluate") {
		http.NotFound(w, r)
		return
	}
	ctx, finish, admitted := s.sessions.begin(r.Context())
	if !admitted {
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	defer finish()
	r = r.WithContext(ctx)
	if rpc {
		s.execute(w, r)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, scriptworker.MaxRequestBytes))
	if err != nil || len(data) == 0 {
		http.Error(w, "invalid request size", http.StatusBadRequest)
		return
	}
	// The parent deadline is independent of the child watchdog and kills/reaps
	// even if the VM or child runtime stops responding.
	ctx, cancel := context.WithTimeout(r.Context(), scriptprotocol.EvaluateHostTimeout)
	defer cancel()
	//nolint:gosec // Fixed operator-owned absolute helper; request supplies stdin only, never executable or arguments.
	command := exec.CommandContext(ctx, s.executable)
	command.Env = []string{}
	command.Stdin = bytes.NewReader(data)
	output := &limitedOutput{}
	command.Stdout = output
	command.Stderr = io.Discard
	command.WaitDelay = scriptprotocol.ProcessWaitDelay
	if err = command.Run(); err != nil || output.overflow {
		http.Error(w, "script process failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(output.data.Bytes())
}

// execute relays one RPC connection to a fresh child. The supervisor never
// interprets business calls and has no application credentials or network.
func (s *Service) execute(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "stream unavailable", http.StatusInternalServerError)
		return
	}
	connection, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(r.Context(), scriptprotocol.ExecuteTransportTimeout)
	defer cancel()
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(closed)
		_ = connection.Close()
	})
	defer func() {
		if !stop() {
			<-closed
		}
	}()
	deadline, _ := ctx.Deadline()
	if connection.SetDeadline(deadline) != nil {
		return
	}
	//nolint:gosec // Fixed operator-owned helper and constant RPC mode; scripts enter stdin only.
	command := exec.CommandContext(ctx, s.executable, "rpc")
	command.Env = []string{}
	command.Stdout = &scriptprotocol.Writer{Output: connection, Remaining: scriptprotocol.MaxTraffic}
	command.Stderr = io.Discard
	command.WaitDelay = scriptprotocol.ProcessWaitDelay
	input, err := command.StdinPipe()
	if err != nil {
		return
	}
	defer input.Close()
	if command.Start() != nil {
		return
	}
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		cancel()
		_ = command.Wait()
		return
	}
	if buffer.Flush() != nil {
		cancel()
		_ = command.Wait()
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(input, io.LimitReader(buffer, scriptprotocol.MaxTraffic))
		_ = input.Close()
	}()
	_ = command.Wait()
	_ = connection.Close()
	<-done
}

type limitedOutput struct {
	data     bytes.Buffer
	overflow bool
}

func (w *limitedOutput) Write(data []byte) (int, error) {
	if len(data) > scriptworker.MaxResponseBytes-w.data.Len() {
		w.overflow = true
		return 0, errors.New("script output exceeds limit")
	}
	return w.data.Write(data)
}
