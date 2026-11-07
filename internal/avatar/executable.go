package avatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const processTimeout = 3 * time.Second
const maxProtocolBytes = 6 * 1024 * 1024
const maxDiagnosticBytes = 4096

type Executable struct {
	path string
	gate chan struct{}
}

// NewExecutable pins a trusted local program. The caller owns its deployment permissions.
func NewExecutable(path string) (*Executable, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrProcessorUnavailable
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrProcessorUnavailable
	}
	return &Executable{path: path, gate: make(chan struct{}, 1)}, nil
}

// Render runs at most one child per processor and never passes the server environment.
func (e *Executable) Render(ctx context.Context, raw []byte, transform Transform) (Rendered, error) {
	transform, err := NormalizeTransform(&transform)
	if err != nil {
		return Rendered{}, err
	}
	if len(raw) == 0 {
		return Rendered{}, ErrInvalidImage
	}
	if len(raw) > MaxInputBytes {
		return Rendered{}, ErrImageLimit
	}
	select {
	case e.gate <- struct{}{}:
		defer func() { <-e.gate }()
	case <-ctx.Done():
		return Rendered{}, ctx.Err()
	}
	child, cancel := context.WithTimeout(ctx, processTimeout)
	defer cancel()
	cmd := exec.CommandContext(child, e.path) // #nosec G204 -- trusted absolute deployment path; no user arguments.
	cmd.Env = []string{"GOMAXPROCS=1", "GOMEMLIMIT=128MiB", "LANG=C"}
	input, err := renderInput(raw, transform)
	if err != nil {
		return Rendered{}, err
	}
	cmd.Stdin = input
	stdout := &limitedOutput{remaining: maxProtocolBytes}
	stderr := &limitedDiagnostics{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if child.Err() != nil {
		return Rendered{}, child.Err()
	}
	if stdout.exceeded {
		return Rendered{}, ErrImageLimit
	}
	if err != nil {
		return Rendered{}, processError(err)
	}
	return decodeRendered(stdout.Bytes(), raw, transform)
}

func processError(err error) error {
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		switch exit.ExitCode() {
		case ExitInvalidImage:
			return ErrInvalidImage
		case ExitImageLimit:
			return ErrImageLimit
		}
	}
	return ErrProcessorUnavailable
}

func decodeRendered(protocol, source []byte, transform Transform) (Rendered, error) {
	var result Rendered
	decoder := json.NewDecoder(bytes.NewReader(protocol))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return Rendered{}, ErrProcessorUnavailable
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return Rendered{}, ErrProcessorUnavailable
	}
	if len(result.JPEG) == 0 || len(result.JPEG) > MaxOutputBytes {
		return Rendered{}, ErrImageLimit
	}
	sourceHash, outputHash := sha256.Sum256(source), sha256.Sum256(result.JPEG)
	if result.Width != OutputSize || result.Height != OutputSize || result.FrameSHA256 != FrameSHA256 ||
		!result.MatchesTransform(transform) || result.SourceSHA256 != hex.EncodeToString(sourceHash[:]) ||
		result.SHA256 != hex.EncodeToString(outputHash[:]) || !bytes.HasPrefix(result.JPEG, []byte{0xff, 0xd8}) {
		return Rendered{}, ErrProcessorUnavailable
	}
	return result, nil
}

type limitedOutput struct {
	buffer    bytes.Buffer
	remaining int
	exceeded  bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.remaining {
		b.exceeded = true
		return 0, ErrImageLimit
	}
	b.remaining -= len(p)
	return b.buffer.Write(p)
}

func (b *limitedOutput) Bytes() []byte { return b.buffer.Bytes() }

type limitedDiagnostics struct{ data []byte }

func (b *limitedDiagnostics) Write(p []byte) (int, error) {
	remaining := min(maxDiagnosticBytes-len(b.data), len(p))
	b.data = append(b.data, p[:remaining]...)
	return len(p), nil
}
