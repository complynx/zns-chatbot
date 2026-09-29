package stickermedia

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"time"
)

type animation struct {
	Width  int     `json:"w"`
	Height int     `json:"h"`
	Rate   float64 `json:"fr"`
	Start  float64 `json:"ip"`
	End    float64 `json:"op"`
}

func unpackTGS(data []byte) ([]byte, animation, error) {
	var info animation
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, info, ErrInvalid
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, maxJSONBytes+1))
	if len(body) > maxJSONBytes {
		return nil, info, ErrLimit
	}
	var tree map[string]any
	if err != nil || json.Unmarshal(body, &tree) != nil || !safeNode(tree, 0) {
		return nil, info, ErrInvalid
	}
	info, err = metadata(tree)
	if err != nil {
		return nil, info, err
	}
	if info.Width < 1 || info.Width > maxSide || info.Height < 1 || info.Height > maxSide ||
		info.Rate < 1 || info.Rate > 60 || info.Start < 0 || info.Start > 180 || info.End <= info.Start ||
		info.End-info.Start > info.Rate*3 || math.Trunc(info.Start) != info.Start || math.Trunc(info.End) != info.End {
		return nil, info, ErrInvalid
	}
	// Canonicalize after validation so duplicate keys cannot be interpreted differently
	// by Go's JSON parser and the native renderer.
	canonical, err := json.Marshal(tree)
	if err != nil {
		return nil, info, ErrInvalid
	}
	return canonical, info, nil
}

// Select exact keys from the object sent to rlottie. Direct struct unmarshaling
// also accepts case variants, which rlottie does not interpret as metadata.
func metadata(tree map[string]any) (animation, error) {
	var info animation
	fields := make(map[string]any)
	for _, key := range []string{"w", "h", "fr", "ip", "op"} {
		value, exists := tree[key]
		if !exists {
			return info, ErrInvalid
		}
		fields[key] = value
	}
	data, err := json.Marshal(fields)
	if err != nil || json.Unmarshal(data, &info) != nil {
		return info, ErrInvalid
	}
	return info, nil
}

// Telegram TGS contains vector shapes and precompositions, never external images.
// Reject image resources, font resources, expressions and image layers recursively.
func safeNode(node any, depth int) bool {
	if depth > maxJSONDepth {
		return false
	}
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if !safeProperty(key, child) || !safeNode(child, depth+1) {
				return false
			}
		}
	case []any:
		for _, child := range value {
			if !safeNode(child, depth+1) {
				return false
			}
		}
	}
	return true
}

func (n Normalizer) renderTGS(ctx context.Context, data []byte) ([]Frame, error) {
	body, info, err := unpackTGS(data)
	if err != nil {
		return nil, err
	}
	count := min(frameCount, int(info.End-info.Start))
	frames := make([]Frame, 0, count)
	for index := range count {
		frameIndex := index * int(info.End-info.Start) / count
		raw, renderErr := run(ctx, n.TGSRenderer, body, maxSide*maxSide*bytesPerPixel, strconv.Itoa(frameIndex))
		if renderErr != nil {
			return nil, renderErr
		}
		frame, encodeErr := encodeRGBA(
			raw,
			info.Width,
			info.Height,
			time.Duration(float64(frameIndex)/info.Rate*float64(time.Second)),
		)
		if encodeErr != nil {
			return nil, encodeErr
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func safeProperty(key string, child any) bool {
	if key == "u" || key == "fonts" || key == "chars" {
		return false
	}
	_, text := child.(string)
	if (key == "p" || key == "x") && text {
		return false
	}
	return key != "ty" || child != float64(imageLayerType)
}
