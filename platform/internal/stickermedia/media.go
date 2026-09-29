package stickermedia

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

type probedFrame struct {
	Timestamp string `json:"best_effort_timestamp_time"`
	Duration  string `json:"duration_time"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

func mediaInput(path string, format Format, extract bool) []string {
	demuxer := "webp_pipe"
	args := []string{"-v", "error", "-protocol_whitelist", "file,pipe", "-threads", "1"}
	if format == WebM {
		demuxer = "matroska"
		if extract {
			// FFmpeg's native VP9 decoder does not read WebM auxiliary alpha.
			args = append(args, "-c:v", "libvpx-vp9")
		}
	}
	return append(args, "-f", demuxer, "-i", path)
}

func (n Normalizer) renderMedia(ctx context.Context, path string, format Format) ([]Frame, error) {
	args := append(
		mediaInput(path, format, false),
		"-select_streams",
		"v:0",
		"-show_frames",
		"-show_entries",
		"frame=best_effort_timestamp_time,duration_time,width,height",
		"-of",
		"json",
	)
	raw, err := run(ctx, n.FFprobe, nil, maxProbeBytes, args...)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Frames []probedFrame `json:"frames"`
	}
	if json.Unmarshal(raw, &probe) != nil || len(probe.Frames) == 0 || len(probe.Frames) > 180 {
		return nil, ErrInvalid
	}
	times, err := frameTimes(probe.Frames, format)
	if err != nil {
		return nil, err
	}
	return n.extractFrames(ctx, path, format, times)
}
func frameTimes(frames []probedFrame, format Format) ([]time.Duration, error) {
	times := make([]time.Duration, len(frames))
	for index, frame := range frames {
		if frame.Width < 1 || frame.Height < 1 || frame.Width > maxSide || frame.Height > maxSide {
			return nil, ErrLimit
		}
		if format == WebP {
			continue
		}
		seconds, parseErr := strconv.ParseFloat(frame.Timestamp, 64)
		if parseErr != nil || !(seconds >= 0 && seconds < 3) {
			return nil, ErrInvalid
		}
		duration, durationErr := strconv.ParseFloat(frame.Duration, 64)
		if durationErr != nil || !(duration > 0 && seconds+duration <= 3) {
			return nil, ErrInvalid
		}
		times[index] = time.Duration(seconds * float64(time.Second))
		if index > 0 && times[index] <= times[index-1] {
			return nil, ErrInvalid
		}
	}
	return times, nil
}

func (n Normalizer) extractFrames(
	ctx context.Context,
	path string,
	format Format,
	times []time.Duration,
) ([]Frame, error) {
	count := min(frameCount, len(times))
	if format == WebP {
		count = 1
	}
	frames := make([]Frame, 0, count)
	for index := range count {
		selected := index * len(times) / count
		filter := "select=eq(n\\," + strconv.Itoa(selected) + ")"
		args := append(
			mediaInput(path, format, true),
			"-filter_threads",
			"1",
			"-threads",
			"1",
			"-map",
			"0:v:0",
			"-an",
			"-sn",
			"-dn",
			"-vf",
			filter,
			"-frames:v",
			"1",
			"-f",
			"image2pipe",
			"-c:v",
			"png",
			"pipe:1",
		)
		data, decodeErr := run(ctx, n.FFmpeg, nil, maxFrameBytes, args...)
		if decodeErr != nil {
			return nil, decodeErr
		}
		frame, frameErr := pngFrame(data, times[selected])
		if frameErr != nil {
			return nil, frameErr
		}
		frames = append(frames, frame)
	}
	return frames, nil
}
