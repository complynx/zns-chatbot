package mediaproc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
)

type timeline struct {
	origin   *big.Rat
	end      *big.Rat
	previous map[int]*big.Rat
	count    map[int]int
}

func (worker *Worker) scanFrames(ctx context.Context, path string, info *admission) (admission, string) {
	child, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	args := append(
		inputArgs(path),
		"-show_frames",
		"-show_entries",
		"frame=stream_index,pts,duration,pkt_duration,nb_samples,width,height",
		"-of",
		"json",
	)
	command := mediaCommand(child, worker.config.FFprobePath, args)
	stderr := limitedOutput{remaining: maxStderrBytes, cancel: cancel}
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	if err != nil {
		return admission{}, reasonDuration
	}
	if err = command.Start(); err != nil {
		return admission{}, reasonDuration
	}
	state := timeline{previous: map[int]*big.Rat{}, count: map[int]int{}}
	bounded := scanReader{reader: output, remaining: maxScanBytes, cancel: cancel}
	reason := decodeFrames(&bounded, info, &state)
	if reason != "" {
		cancel()
	}
	waitErr := command.Wait()
	if reason != "" {
		return admission{}, reason
	}
	if waitErr != nil || stderr.Len() != 0 {
		return admission{}, reasonDuration
	}
	return info.finishMeasure(state.origin, state.end, state.count)
}

type scanReader struct {
	reader    io.Reader
	remaining int
	cancel    context.CancelFunc
}

func (reader *scanReader) Read(buffer []byte) (int, error) {
	if len(buffer) > reader.remaining+1 {
		buffer = buffer[:reader.remaining+1]
	}
	n, err := reader.reader.Read(buffer)
	if n > reader.remaining {
		reader.cancel()
		return 0, errors.New("frame metadata budget exceeded")
	}
	reader.remaining -= n
	return n, err
}

// decodeFrames admits only a complete JSON frame stream, or returns an early rejection.
func decodeFrames(reader io.Reader, info *admission, state *timeline) string {
	decoder := json.NewDecoder(reader)
	if !expectToken(decoder, json.Delim('{')) || !expectToken(decoder, "frames") ||
		!expectToken(decoder, json.Delim('[')) {
		return reasonDuration
	}
	for decoder.More() {
		var frame decodedFrame
		if decoder.Decode(&frame) != nil {
			return reasonDuration
		}
		if reason := state.accept(info, frame); reason != "" {
			return reason
		}
	}
	if !expectToken(decoder, json.Delim(']')) || !expectToken(decoder, json.Delim('}')) {
		return reasonDuration
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return reasonDuration
	}
	return ""
}
func expectToken(decoder *json.Decoder, expected any) bool {
	token, err := decoder.Token()
	return err == nil && token == expected
}
func (state *timeline) accept(info *admission, frame decodedFrame) string {
	if frame.Stream != info.audio && frame.Stream != info.video {
		return ""
	}
	start, finish, err := frameTime(info.streamByIndex(frame.Stream), frame)
	if err != nil {
		return reasonDuration
	}
	if last := state.previous[frame.Stream]; last != nil && start.Cmp(last) <= 0 {
		return reasonDuration
	}
	state.previous[frame.Stream] = start
	state.count[frame.Stream]++
	if !info.recordVideo(frame, start) {
		return "resource_limit"
	}
	if state.origin == nil || start.Cmp(state.origin) < 0 {
		state.origin = new(big.Rat).Set(start)
	}
	if state.end == nil || finish.Cmp(state.end) > 0 {
		state.end = new(big.Rat).Set(finish)
	}
	if new(big.Rat).Sub(state.end, state.origin).Cmp(big.NewRat(maxDuration, 1)) > 0 {
		return "too_long"
	}
	return ""
}
