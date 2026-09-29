package mediaproc

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func (worker *Worker) extractAudio(ctx context.Context, path string, audio int) ([]byte, Transcript) {
	if audio < 0 {
		return nil, Transcript{Status: "no_audio"}
	}
	args := append(
		extractionArgs(path),
		"-map",
		"0:"+strconv.Itoa(audio),
		"-vn",
		"-sn",
		"-dn",
		"-map_metadata",
		"-1",
		"-ac",
		"1",
		"-ar",
		"16000",
		"-c:a",
		"pcm_s16le",
		"-f",
		"wav",
		"pipe:1",
	)
	wav, err := run(ctx, worker.config.FFmpegPath, audioTimeout, maxWAVBytes, args...)
	if err != nil {
		return nil, Transcript{Status: statusFailed}
	}
	if isSilentWAV(wav) {
		return nil, Transcript{Status: "silent"}
	}
	return wav, Transcript{Status: transcriptNotRun}
}

func (worker *Worker) recognize(ctx context.Context, wav []byte) Transcript {
	if worker.config.APIKey == "" {
		return Transcript{Status: statusFailed}
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="audio.wav"`)
	header.Set("Content-Type", "audio/wav")
	part, err := form.CreatePart(header)
	if err != nil {
		return Transcript{Status: statusFailed}
	}
	if _, err = part.Write(wav); err != nil {
		return Transcript{Status: statusFailed}
	}
	if form.WriteField("model", worker.config.Model) != nil || form.Close() != nil {
		return Transcript{Status: statusFailed}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, worker.config.APIURL, &body)
	if err != nil {
		return Transcript{Status: statusFailed}
	}
	request.Header.Set("Authorization", "Bearer "+worker.config.APIKey)
	request.Header.Set("Content-Type", form.FormDataContentType())
	call, err := credits.Begin(ctx, worker.config.Accounting, "audio.transcribe", "openai", worker.config.Model)
	if err != nil {
		return Transcript{Status: statusFailed}
	}
	result := worker.readTranscript(request, call)
	if call.Finish(ctx) != nil {
		return Transcript{Status: statusFailed}
	}
	return result
}

func (worker *Worker) readTranscript(request *http.Request, call *credits.Call) Transcript {
	singleSend := *worker.config.HTTPClient
	singleSend.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := singleSend.Do(request)
	if err != nil {
		return Transcript{Status: statusFailed}
	}
	defer response.Body.Close()
	call.CaptureRequestID(response.Header.Get("X-Request-ID"))
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderBytes+1))
	if err != nil || len(data) > maxProviderBytes || !utf8.Valid(data) {
		return Transcript{Status: statusFailed}
	}
	call.Capture(credits.TranscriptionUsage(data, worker.config.Model))
	if response.StatusCode != http.StatusOK {
		return Transcript{Status: statusFailed}
	}
	var decoded struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(data, &decoded) != nil || decoded.Text == nil || !utf8.ValidString(*decoded.Text) ||
		len(*decoded.Text) > maxTranscriptBytes {
		return Transcript{Status: statusFailed}
	}
	if strings.TrimSpace(*decoded.Text) == "" {
		return Transcript{Status: "no_text"}
	}
	return Transcript{Status: "ok", Text: *decoded.Text}
}

func (worker *Worker) storyboard(
	ctx context.Context,
	path string,
	info admission,
	start, end *big.Rat,
	count int,
	coverage string,
) ([]Frame, *Sampling) {
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	frames := []Frame{}
	sampling := &Sampling{Coverage: coverage, Requested: []Rational{}}
	step := new(big.Rat).Quo(new(big.Rat).Sub(end, start), big.NewRat(int64(count), 1))
	last := -1
	for index := range count {
		target := new(big.Rat).Add(start, new(big.Rat).Mul(step, big.NewRat(int64(index), 1)))
		sampling.Requested = append(sampling.Requested, rational(target))
		selected := -1
		for i, timestamp := range info.videoTimes {
			if timestamp.Cmp(target) >= 0 && timestamp.Cmp(end) < 0 {
				selected = i
				break
			}
		}
		if selected < 0 || selected == last {
			continue
		}
		last = selected
		filter := "select=eq(n\\," + strconv.Itoa(
			selected,
		) + "),scale=768:768:force_original_aspect_ratio=decrease,setsar=1"
		args := append(
			extractionArgs(path),
			"-map",
			"0:"+strconv.Itoa(info.video),
			"-an",
			"-sn",
			"-dn",
			"-vf",
			filter,
			"-frames:v",
			"1",
			"-q:v",
			"5",
			"-f",
			"image2pipe",
			"-vcodec",
			"mjpeg",
			"pipe:1",
		)
		data, err := run(ctx, worker.config.FFmpegPath, scanTimeout, maxFrameBytes, args...)
		if err != nil {
			return nil, sampling
		}
		dimensions, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || dimensions.Width > 768 || dimensions.Height > 768 {
			return nil, sampling
		}
		frames = append(
			frames,
			Frame{
				JPEG:      data,
				Width:     dimensions.Width,
				Height:    dimensions.Height,
				Timestamp: rational(info.videoTimes[selected]),
			},
		)
	}
	return frames, sampling
}

// extractionArgs bounds decoder, filter graph and output encoder threads separately.
func extractionArgs(path string) []string {
	return append(inputArgs(path), "-filter_threads", "2", "-filter_complex_threads", "2", "-threads", "2")
}
