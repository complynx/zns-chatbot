package mediaproc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
)

type stream struct {
	Index       int    `json:"index"`
	Codec       string `json:"codec_name"`
	Type        string `json:"codec_type"`
	TimeBase    string `json:"time_base"`
	Rate        string `json:"sample_rate"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Aspect      string `json:"sample_aspect_ratio"`
	Disposition struct {
		Attached int `json:"attached_pic"`
	} `json:"disposition"`
	SideData []json.RawMessage `json:"side_data_list"`
}
type decodedFrame struct {
	Stream         int    `json:"stream_index"`
	PTS            *int64 `json:"pts"`
	Duration       *int64 `json:"duration"`
	PacketDuration int64  `json:"pkt_duration"`
	Samples        int64  `json:"nb_samples"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
}
type admission struct {
	duration   *big.Rat
	streams    []stream
	videoTimes []*big.Rat
	audio      int
	video      int
}

func rational(value *big.Rat) Rational {
	return Rational{Numerator: value.Num().Int64(), Denominator: value.Denom().Int64()}
}
func (worker *Worker) inspect(ctx context.Context, path, kind string) (admission, string) {
	args := append(inputArgs(path), "-show_streams", "-of", "json")
	data, err := run(ctx, worker.config.FFprobePath, probeTimeout, maxProbeBytes, args...)
	if err != nil {
		return admission{}, "invalid_media"
	}
	var metadata struct {
		Streams []stream `json:"streams"`
	}
	if json.Unmarshal(data, &metadata) != nil {
		return admission{}, "invalid_media"
	}
	info := admission{streams: metadata.Streams, audio: -1, video: -1}
	if reason := info.validateStreams(kind); reason != "" {
		return admission{}, reason
	}
	return worker.scanFrames(ctx, path, &info)
}
func (info *admission) validateStreams(kind string) string {
	if len(info.streams) == 0 || len(info.streams) > 8 {
		return reasonUnsupported
	}
	for _, entry := range info.streams {
		if entry.Disposition.Attached != 0 {
			continue
		}
		if reason := info.validateStream(entry); reason != "" {
			return reason
		}
	}
	if (kind == kindAudio || kind == "voice") && (info.audio < 0 || info.video >= 0) {
		return reasonUnsupported
	}
	if (kind == kindVideo || kind == "video_note") && info.video < 0 {
		return reasonUnsupported
	}
	return ""
}
func (info *admission) validateStream(entry stream) string {
	switch entry.Type {
	case kindAudio:
		if info.audio >= 0 {
			return reasonUnsupported
		}
		info.audio = entry.Index
		switch entry.Codec {
		case "pcm_s16le", "pcm_s24le", "pcm_f32le", "aac", "opus", "mp3":
		default:
			return reasonUnsupported
		}
	case kindVideo:
		if entry.Aspect != "" && entry.Aspect != "1:1" && entry.Aspect != "N/A" {
			return reasonUnsupported
		}
		if info.video >= 0 || entry.Width <= 0 || entry.Height <= 0 || entry.Width > 3840 || entry.Height > 2160 ||
			!supportedDisplayRotation(entry.SideData) {
			return reasonUnsupported
		}
		switch entry.Codec {
		case "h264", "mpeg4", "vp8", "vp9":
		default:
			return reasonUnsupported
		}
		info.video = entry.Index
	default:
		return reasonUnsupported
	}
	return ""
}
func frameTime(entry stream, frame decodedFrame) (*big.Rat, *big.Rat, error) {
	base, ok := new(big.Rat).SetString(entry.TimeBase)
	if !ok || base.Sign() <= 0 || frame.PTS == nil {
		return nil, nil, errors.New("missing timestamp")
	}
	start := new(big.Rat).Mul(new(big.Rat).SetInt64(*frame.PTS), base)
	duration := new(big.Rat).Mul(new(big.Rat).SetInt64(frame.presentationDuration()), base)
	if entry.Type == kindAudio {
		rate, err := strconv.ParseInt(entry.Rate, 10, 64)
		if err != nil || rate <= 0 || frame.Samples <= 0 {
			return nil, nil, errors.New("missing sample duration")
		}
		samples := big.NewRat(frame.Samples, rate)
		if entry.Codec != "aac" || duration.Sign() <= 0 || duration.Cmp(samples) > 0 {
			duration.Set(samples)
		}
	}
	if duration.Sign() <= 0 {
		return nil, nil, errors.New("missing endpoint")
	}
	return start, new(big.Rat).Add(start, duration), nil
}
func (info *admission) streamByIndex(index int) stream {
	for _, entry := range info.streams {
		if entry.Index == index {
			return entry
		}
	}
	return stream{}
}
func (info *admission) recordVideo(frame decodedFrame, start *big.Rat) bool {
	if frame.Stream != info.video {
		return true
	}
	if frame.Width <= 0 || frame.Height <= 0 || frame.Width > 3840 || frame.Height > 2160 {
		return false
	}
	info.videoTimes = append(info.videoTimes, start)
	return true
}
func (info *admission) finishMeasure(origin, end *big.Rat, count map[int]int) (admission, string) {
	if origin == nil || (info.audio >= 0 && count[info.audio] == 0) || (info.video >= 0 && count[info.video] == 0) {
		return admission{}, reasonDuration
	}
	info.duration = new(big.Rat).Sub(end, origin)
	if info.duration.Sign() <= 0 {
		return admission{}, reasonDuration
	}
	if info.video >= 0 &&
		new(
			big.Rat,
		).SetFrac64(int64(count[info.video]), 1).
			Cmp(new(big.Rat).Mul(info.duration, big.NewRat(maxFPS, 1))) >
			0 {
		return admission{}, "resource_limit"
	}
	// One shared origin preserves gaps and relative audio/video offsets.
	// Frame indices stay unchanged for extraction from the original file.
	for _, timestamp := range info.videoTimes {
		timestamp.Sub(timestamp, origin)
	}
	return *info, ""
}
func (frame decodedFrame) presentationDuration() int64 {
	if frame.Duration != nil {
		return *frame.Duration
	}
	return frame.PacketDuration
}
