package mediaproc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/jpeg"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

// DecoderAuthorization is a protocol marker; access is controlled by Unix socket permissions.
const DecoderAuthorization = "unix-local-decoder"
const maxDecoderResponse = 16 << 20

type decodedOutput struct {
	Result Result `json:"result"`
	WAV    []byte `json:"wav,omitempty"`
}
type BrokerConfig struct {
	Accounting     credits.Recorder
	CreditsEnforce bool
	Secret         string
	APIKey         string
	Model          string
	APIURL         string
	SocketPath     string
	HTTPClient     *http.Client
}
type Broker struct {
	creditsEnforce bool
	secret         string
	decoder        *http.Client
	recognizer     *Worker
	slots          chan struct{}
}

func NewBroker(config BrokerConfig) (*Broker, error) {
	if config.Secret == "" || !filepath.IsAbs(config.SocketPath) {
		return nil, errors.New("broker requires a secret and absolute decoder socket path")
	}
	recognizer := &Worker{
		config: Config{
			Accounting: config.Accounting,
			APIKey:     config.APIKey,
			Model:      config.Model,
			APIURL:     config.APIURL,
			HTTPClient: config.HTTPClient,
		},
	}
	if recognizer.config.Model == "" {
		recognizer.config.Model = "gpt-transcribe"
	}
	if recognizer.config.APIURL == "" {
		recognizer.config.APIURL = "https://api.openai.com/v1/audio/transcriptions"
	}
	if recognizer.config.HTTPClient == nil {
		recognizer.config.HTTPClient = &http.Client{Timeout: asrTimeout}
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", config.SocketPath)
	}}
	return &Broker{
		secret:         config.Secret,
		creditsEnforce: config.CreditsEnforce,
		decoder: &http.Client{
			Transport:     transport,
			Timeout:       jobTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		recognizer: recognizer,
		slots:      make(chan struct{}, concurrentJobs),
	}, nil
}

func (broker *Broker) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost ||
		(request.URL.Path != preprocessPath && request.URL.Path != storyboardPath) {
		http.NotFound(writer, request)
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+broker.secret)) != 1 {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	select {
	case broker.slots <- struct{}{}:
		defer func() { <-broker.slots }()
	default:
		http.Error(writer, "busy", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), jobTimeout)
	defer cancel()
	scope, scopeErr := credits.AuthenticatedScope(request)
	if scopeErr != nil {
		http.Error(writer, "invalid accounting scope", http.StatusBadRequest)
		return
	}
	ctx = credits.WithScope(ctx, scope)
	mode, modeErr := credits.RequestMode(request)
	enforced := broker.creditsEnforce
	if modeErr != nil || mode != enforced {
		http.Error(writer, "accounting mode mismatch", http.StatusServiceUnavailable)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, MaxInputBytes))
	if err != nil || len(data) == 0 {
		http.Error(writer, "invalid size", http.StatusRequestEntityTooLarge)
		return
	}
	result, status := broker.decode(ctx, request.URL, data)
	if status != http.StatusOK {
		http.Error(writer, "decoder unavailable", status)
		return
	}
	broker.addTranscript(ctx, &result)
	writer.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(writer).Encode(result.Result); err != nil {
		return
	}
}

func (broker *Broker) decode(ctx context.Context, source *url.URL, data []byte) (decodedOutput, int) {
	query := url.Values{}
	for _, key := range []string{"kind", "start_ms", "end_ms", "count"} {
		if value := source.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://decoder/v1/preprocess",
		bytes.NewReader(data),
	)
	if err != nil {
		return decodedOutput{}, http.StatusServiceUnavailable
	}
	if source.Path == storyboardPath {
		request.URL.Path = storyboardPath
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+DecoderAuthorization)
	response, err := broker.decoder.Do(request)
	if err != nil {
		return decodedOutput{}, http.StatusServiceUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return decodedOutput{}, response.StatusCode
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDecoderResponse+1))
	if err != nil || len(body) > maxDecoderResponse {
		return decodedOutput{}, http.StatusBadGateway
	}
	var output decodedOutput
	if json.Unmarshal(body, &output) != nil || !validDecodedOutput(output, source.Path) {
		return decodedOutput{}, http.StatusBadGateway
	}
	return output, http.StatusOK
}

func (broker *Broker) addTranscript(ctx context.Context, result *decodedOutput) {
	if len(result.WAV) == 0 {
		return
	}
	result.Result.Transcript = broker.recognizer.recognize(ctx, result.WAV)
	if result.Result.Transcript.Status != statusFailed {
		return
	}
	result.Result.Status = statusFailed
	result.Result.Reason = "transcription_failed"
	if broker.recognizer.config.APIKey == "" {
		result.Result.Reason = "transcription_unavailable"
	}
	if len(result.Result.Frames) > 0 {
		result.Result.Status = statusPartial
	}
}
func validDecodedOutput(output decodedOutput, path string) bool {
	if len(output.WAV) > maxWAVBytes || len(output.Result.Frames) > maxFrames {
		return false
	}
	if output.Result.Transcript.Text != "" {
		return false
	}
	if output.Result.Status == statusRejected {
		return len(output.WAV) == 0 && len(output.Result.Frames) == 0 &&
			output.Result.Transcript.Status == transcriptNotRun
	}
	if output.Result.Status != "ready" && output.Result.Status != statusFailed &&
		output.Result.Status != statusPartial {
		return false
	}
	duration := output.Result.Duration
	if duration.Denominator <= 0 || duration.Numerator <= 0 {
		return false
	}
	extent := big.NewRat(duration.Numerator, duration.Denominator)
	if extent.Cmp(big.NewRat(maxDuration, 1)) > 0 {
		return false
	}
	for _, frame := range output.Result.Frames {
		if !validDecodedFrame(frame, extent) {
			return false
		}
	}
	if len(output.WAV) == 0 {
		if path == storyboardPath {
			return output.Result.Transcript.Status == transcriptNotRun
		}
		return output.Result.Transcript.Status == "silent" || output.Result.Transcript.Status == "no_audio" ||
			output.Result.Transcript.Status == statusFailed
	}
	return path == preprocessPath && output.Result.Status != statusRejected &&
		output.Result.Transcript.Status == transcriptNotRun &&
		validNormalizedWAV(output.WAV)
}

func validDecodedFrame(frame Frame, extent *big.Rat) bool {
	if len(frame.JPEG) > maxFrameBytes || frame.Timestamp.Denominator <= 0 || frame.Timestamp.Numerator < 0 {
		return false
	}
	if big.NewRat(frame.Timestamp.Numerator, frame.Timestamp.Denominator).Cmp(extent) >= 0 {
		return false
	}
	dimensions, err := jpeg.DecodeConfig(bytes.NewReader(frame.JPEG))
	return err == nil && dimensions.Width == frame.Width && dimensions.Height == frame.Height && frame.Width > 0 &&
		frame.Height > 0 &&
		frame.Width <= 768 &&
		frame.Height <= 768
}

func validNormalizedWAV(wav []byte) bool {
	const headerBytes = 12
	const formatBytes = 16
	const sampleRate = 16000
	const sampleBytes = 2
	const sampleBits = 16
	if len(wav) < headerBytes || string(wav[:4]) != "RIFF" || string(wav[8:headerBytes]) != "WAVE" {
		return false
	}
	validFormat := false
	for offset := headerBytes; offset+chunkHeaderBytes <= len(wav); {
		size := int64(binary.LittleEndian.Uint32(wav[offset+4 : offset+chunkHeaderBytes]))
		start := offset + chunkHeaderBytes
		switch string(wav[offset : offset+4]) {
		case "fmt ":
			if size < formatBytes || size > int64(len(wav)-start) {
				return false
			}
			format := wav[start:]
			validFormat = binary.LittleEndian.Uint16(format[:2]) == 1 && binary.LittleEndian.Uint16(format[2:4]) == 1 &&
				binary.LittleEndian.Uint32(format[4:8]) == sampleRate &&
				binary.LittleEndian.Uint16(format[12:14]) == sampleBytes &&
				binary.LittleEndian.Uint16(format[14:16]) == sampleBits
		case "data":
			payload := len(wav) - start
			return validFormat && payload > 0 && payload%sampleBytes == 0 &&
				payload <= maxWAVBytes
		}
		if size > int64(len(wav)-start) {
			return false
		}
		offset = start + int(size) + int(size%chunkAlignment)
	}
	return false
}
