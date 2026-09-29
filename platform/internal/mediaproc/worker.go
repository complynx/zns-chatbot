package mediaproc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

func (worker *Worker) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost ||
		(request.URL.Path != preprocessPath && request.URL.Path != storyboardPath) {
		http.NotFound(writer, request)
		return
	}
	if subtle.ConstantTimeCompare(
		[]byte(request.Header.Get("Authorization")),
		[]byte("Bearer "+worker.config.Secret),
	) != 1 {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	kind := request.URL.Query().Get("kind")
	if kind != kindAudio && kind != "voice" && kind != kindVideo && kind != "video_note" {
		http.Error(writer, "invalid kind", http.StatusBadRequest)
		return
	}
	select {
	case worker.slots <- struct{}{}:
		defer func() { <-worker.slots }()
	default:
		http.Error(writer, "busy", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), jobTimeout)
	defer cancel()
	data, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, MaxInputBytes))
	if err != nil || len(data) == 0 {
		http.Error(writer, "invalid size", http.StatusRequestEntityTooLarge)
		return
	}
	directory, err := os.MkdirTemp(worker.config.TempRoot, "media-")
	if err != nil {
		http.Error(writer, "temporary storage unavailable", http.StatusServiceUnavailable)
		return
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "input")
	if os.WriteFile(path, data, 0600) != nil {
		http.Error(writer, "temporary storage unavailable", http.StatusServiceUnavailable)
		return
	}
	result := worker.process(ctx, path, kind, request)
	writer.Header().Set("Content-Type", "application/json")
	if worker.config.DecodeOnly {
		if err = json.NewEncoder(writer).Encode(decodedOutput{Result: result, WAV: result.wav}); err != nil {
			return
		}
		return
	}
	if err = json.NewEncoder(writer).Encode(result); err != nil {
		return
	}
}
func (worker *Worker) process(ctx context.Context, path, kind string, request *http.Request) Result {
	info, reason := worker.inspect(ctx, path, kind)
	if reason != "" {
		return rejected(reason)
	}
	result := Result{
		Status:     "ready",
		Duration:   rational(info.duration),
		Transcript: Transcript{Status: transcriptNotRun},
	}
	start := new(big.Rat)
	end := new(big.Rat).Set(info.duration)
	count := int(
		new(
			big.Int,
		).Quo(new(big.Int).Add(info.duration.Num(), new(big.Int).Sub(new(big.Int).Mul(info.duration.Denom(), big.NewInt(sampleInterval)), big.NewInt(1))), new(big.Int).Mul(info.duration.Denom(), big.NewInt(sampleInterval))).
			Int64(),
	)
	count = min(maxFrames, max(1, count))
	coverage := "uniform_sparse"
	if request.URL.Path == storyboardPath {
		var ok bool
		start, end, count, ok = parseRange(request, info.duration)
		if !ok || info.video < 0 {
			return rejected("invalid_range")
		}
		coverage = "range_sparse"
	} else {
		result.wav, result.Transcript = worker.extractAudio(ctx, path, info.audio)
		if len(result.wav) > 0 && !worker.config.DecodeOnly {
			result.Transcript = worker.recognize(ctx, result.wav)
			result.wav = nil
		}
	}
	if info.video >= 0 {
		result.Frames, result.Sampling = worker.storyboard(ctx, path, info, start, end, count, coverage)
		if len(result.Frames) == 0 {
			result.Status = statusFailed
			result.Reason = "storyboard_failed"
		}
	}
	if result.Transcript.Status == statusFailed {
		result.Status = statusFailed
		result.Reason = "transcription_failed"
		if worker.config.APIKey == "" {
			result.Reason = "transcription_unavailable"
		}
		if len(result.Frames) > 0 {
			result.Status = statusPartial
		}
	}
	return result
}
func parseRange(request *http.Request, duration *big.Rat) (*big.Rat, *big.Rat, int, bool) {
	begin, e1 := strconv.ParseInt(request.URL.Query().Get("start_ms"), 10, 64)
	finish, e2 := strconv.ParseInt(request.URL.Query().Get("end_ms"), 10, 64)
	count, e3 := strconv.Atoi(request.URL.Query().Get("count"))
	start := big.NewRat(begin, millisPerSecond)
	end := big.NewRat(finish, millisPerSecond)
	valid := e1 == nil && e2 == nil && e3 == nil && begin >= 0 && finish > begin && end.Cmp(duration) <= 0 &&
		count >= 1 &&
		count <= maxFrames
	return start, end, count, valid
}
func isSilentWAV(data []byte) bool {
	// RIFF chunks can include LIST metadata before the PCM data chunk.
	if len(data) < 12 || !bytes.Equal(data[:4], []byte("RIFF")) {
		return false
	}
	for offset := 12; offset+8 <= len(data); {
		size := int(
			uint32(
				data[offset+4],
			) | uint32(
				data[offset+5],
			)<<8 | uint32(
				data[offset+6],
			)<<16 | uint32(
				data[offset+7],
			)<<24,
		)
		if bytes.Equal(data[offset:offset+4], []byte("data")) {
			payload := data[offset+8:]
			if len(payload) == 0 {
				return false
			}
			for _, sample := range payload {
				if sample != 0 {
					return false
				}
			}
			return true
		}
		if size > len(data)-offset-8 {
			return false
		}
		offset += chunkHeaderBytes + size + (size % chunkAlignment)
	}
	return false
}
