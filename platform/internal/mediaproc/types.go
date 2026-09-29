package mediaproc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

const (
	reasonDuration    = "unreadable_duration"
	reasonUnsupported = "unsupported"
	kindAudio         = "audio"
	kindVideo         = "video"
	statusFailed      = "failed"
	probeTimeout      = 5 * time.Second
	scanTimeout       = 30 * time.Second
	audioTimeout      = 15 * time.Second
	asrTimeout        = 60 * time.Second
	jobTimeout        = 150 * time.Second
	maxProviderBytes  = 256 << 10
	maxStderrBytes    = 64 << 10
	concurrentJobs    = 2
	millisPerSecond   = 1000
	chunkHeaderBytes  = 8
)
const MaxInputBytes = 20 << 20
const maxTranscriptBytes = 64 << 10
const maxFrameBytes = 256 << 10
const maxFrames = 8
const maxDuration = 240

type Rational struct {
	Numerator   int64 `json:"numerator"`
	Denominator int64 `json:"denominator"`
}
type Transcript struct {
	Status string `json:"status"`
	Text   string `json:"text,omitempty"`
}
type Frame struct {
	JPEG      []byte   `json:"jpeg"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Timestamp Rational `json:"timestamp"`
}
type Sampling struct {
	Coverage  string     `json:"coverage"`
	Requested []Rational `json:"requested"`
}
type Result struct {
	Status     string     `json:"status"`
	Reason     string     `json:"reason,omitempty"`
	Duration   Rational   `json:"duration"`
	Transcript Transcript `json:"transcript"`
	Frames     []Frame    `json:"frames,omitempty"`
	Sampling   *Sampling  `json:"sampling,omitempty"`
	wav        []byte
}
type Config struct {
	Accounting  credits.Recorder
	DecodeOnly  bool
	FFmpegPath  string
	FFprobePath string
	Secret      string
	APIKey      string
	Model       string
	APIURL      string
	TempRoot    string
	HTTPClient  *http.Client
}
type Worker struct {
	config Config
	slots  chan struct{}
}

func New(config Config) (*Worker, error) {
	if config.DecodeOnly && config.APIKey != "" {
		return nil, errors.New("decoder must not receive provider credentials")
	}
	if config.Secret == "" || !filepath.IsAbs(config.FFmpegPath) || !filepath.IsAbs(config.FFprobePath) {
		return nil, errors.New("media worker needs a secret and absolute executable paths")
	}
	if config.Model == "" {
		config.Model = "gpt-transcribe"
	}
	if config.APIURL == "" {
		config.APIURL = "https://api.openai.com/v1/audio/transcriptions"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: asrTimeout}
	}
	return &Worker{config: config, slots: make(chan struct{}, concurrentJobs)}, nil
}

// limitedOutput stops the subprocess when either output budget is exhausted.
type limitedOutput struct {
	bytes.Buffer

	remaining int
	cancel    context.CancelFunc
}

func (output *limitedOutput) Write(data []byte) (int, error) {
	if len(data) > output.remaining {
		output.cancel()
		return 0, errors.New("subprocess output budget exceeded")
	}
	output.remaining -= len(data)
	n, err := output.Buffer.Write(data)
	if err != nil {
		return n, fmt.Errorf("buffer subprocess output: %w", err)
	}
	return n, nil
}
func run(ctx context.Context, executable string, timeout time.Duration, limit int, args ...string) ([]byte, error) {
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := limitedOutput{remaining: limit, cancel: cancel}
	stderr := limitedOutput{remaining: maxStderrBytes, cancel: cancel}
	command := mediaCommand(child, executable, args)
	command.Stdout = &out
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("media command failed: %w", err)
	}
	if stderr.Len() != 0 {
		return nil, errors.New("media decoder reported errors")
	}
	return out.Bytes(), nil
}

// mediaCommand uses only operator-configured executables validated by New.
// Arguments are assembled internally from fixed options and generated input paths.
func mediaCommand(ctx context.Context, executable string, args []string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	if runtime.GOOS == "windows" {
		command.Env = append(command.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return command
}
func inputArgs(path string) []string {
	return []string{
		"-v",
		"error",
		"-threads",
		"2",
		"-protocol_whitelist",
		"file",
		"-format_whitelist",
		"wav,mov,matroska,webm,ogg,mp3",
		"-i",
		path,
	}
}
func rejected(reason string) Result {
	return Result{
		Status:     statusRejected,
		Reason:     reason,
		Duration:   Rational{Denominator: 1},
		Transcript: Transcript{Status: transcriptNotRun},
	}
}

const (
	maxProbeBytes  = 1 << 20
	maxScanBytes   = 16 << 20
	maxWAVBytes    = 8 << 20
	maxFPS         = 60
	sampleInterval = 30
	chunkAlignment = 2
)

const (
	preprocessPath   = "/v1/preprocess"
	storyboardPath   = "/v1/storyboard"
	transcriptNotRun = "not_run"
)

const (
	statusPartial  = "partial"
	statusRejected = "rejected"
)
