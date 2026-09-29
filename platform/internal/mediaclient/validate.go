package mediaclient

import (
	"bytes"
	"image/jpeg"
	"math/big"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

const (
	maxTranscriptBytes = 64 << 10
	maxFrameBytes      = 256 << 10
	maxDimension       = 768
	maxReasonBytes     = 128
	millisPerSecond    = 1000
	sampleInterval     = 30
	statusFailed       = "failed"
)

func rational(value mediaproc.Rational) (*big.Rat, bool) {
	if value.Denominator <= 0 || value.Numerator < 0 {
		return nil, false
	}
	return big.NewRat(value.Numerator, value.Denominator), true
}

func validTranscript(value mediaproc.Transcript, rangeOnly bool) bool {
	if len(value.Text) > maxTranscriptBytes {
		return false
	}
	if rangeOnly {
		return value.Status == "not_run" && value.Text == ""
	}
	switch value.Status {
	case "ok":
		return strings.TrimSpace(value.Text) != ""
	case "no_text", "silent", "no_audio", statusFailed:
		return value.Text == ""
	default:
		return false
	}
}

func validResult(result mediaproc.Result, kind Kind, selection *Range) bool {
	duration, ok := rational(result.Duration)
	if !ok || len(result.Reason) > maxReasonBytes || len(result.Frames) > maxFrames {
		return false
	}
	if result.Status == "rejected" {
		return result.Reason != "" && duration.Sign() == 0 && len(result.Frames) == 0 && result.Sampling == nil &&
			result.Transcript.Status == "not_run" && result.Transcript.Text == ""
	}
	if duration.Sign() <= 0 || duration.Cmp(big.NewRat(maxDurationMS, millisPerSecond)) > 0 ||
		!validTranscript(result.Transcript, selection != nil) || !validStatus(result) {
		return false
	}
	if kind == Audio || kind == Voice {
		return len(result.Frames) == 0 && result.Sampling == nil && result.Status != "partial"
	}
	return validStoryboard(result, duration, selection)
}

func validStatus(result mediaproc.Result) bool {
	switch result.Status {
	case "ready":
		return result.Reason == "" && result.Transcript.Status != statusFailed
	case "partial":
		return result.Reason != "" && result.Transcript.Status == statusFailed && len(result.Frames) > 0
	case statusFailed:
		return result.Reason != "" && len(result.Frames) == 0
	default:
		return false
	}
}

func validStoryboard(result mediaproc.Result, duration *big.Rat, selection *Range) bool {
	if result.Sampling == nil {
		return false
	}
	start, end := new(big.Rat), duration
	coverage := "uniform_sparse"
	// The worker samples at most eight equally spaced targets, one per started 30s.
	count := 1
	for count < maxFrames && duration.Cmp(big.NewRat(int64(count*sampleInterval), 1)) > 0 {
		count++
	}
	if selection != nil {
		start = big.NewRat(selection.StartMS, millisPerSecond)
		end = big.NewRat(selection.EndMS, millisPerSecond)
		count = selection.Count
		coverage = "range_sparse"
		if end.Cmp(duration) > 0 {
			return false
		}
	}
	requested := result.Sampling.Requested
	if result.Sampling.Coverage != coverage || len(requested) < 1 || len(requested) > count ||
		len(result.Frames) > len(requested) {
		return false
	}
	if result.Status != statusFailed && (len(requested) != count || len(result.Frames) == 0) {
		return false
	}
	step := new(big.Rat).Quo(new(big.Rat).Sub(end, start), big.NewRat(int64(count), 1))
	for index, value := range requested {
		timestamp, ok := rational(value)
		expected := new(big.Rat).Add(start, new(big.Rat).Mul(step, big.NewRat(int64(index), 1)))
		if !ok || timestamp.Cmp(expected) != 0 {
			return false
		}
	}
	var previous *big.Rat
	for _, frame := range result.Frames {
		timestamp, ok := rational(frame.Timestamp)
		if !ok || timestamp.Cmp(start) < 0 || timestamp.Cmp(end) >= 0 ||
			(previous != nil && timestamp.Cmp(previous) <= 0) ||
			!validFrame(frame) {
			return false
		}
		previous = timestamp
	}
	return true
}

func validFrame(frame mediaproc.Frame) bool {
	if len(frame.JPEG) == 0 || len(frame.JPEG) > maxFrameBytes || frame.Width < 1 || frame.Height < 1 ||
		frame.Width > maxDimension || frame.Height > maxDimension {
		return false
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(frame.JPEG))
	if err != nil || config.Width != frame.Width || config.Height != frame.Height {
		return false
	}
	_, err = jpeg.Decode(bytes.NewReader(frame.JPEG))
	return err == nil
}
