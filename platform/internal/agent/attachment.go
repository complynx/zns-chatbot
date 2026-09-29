package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
)

const (
	maxAttachmentBytes    = 20 << 20
	maxAttachmentPixels   = 40_000_000
	maxAttachmentMetadata = 1024
	// One initial sample and at most two inspections, each limited to eight frames.
	maxEvidenceFrames = 24
	jpegMIME          = "image/jpeg"
)

// Attachment carries private source bytes between model services. Provider text
// receives metadata only; validated bytes travel through the image input channel.
type Attachment struct {
	TimestampMS int64  `json:"timestamp_ms,omitempty"`
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	MIME        string `json:"mime"`
	Body        []byte `json:"body,omitempty"`
}

func (a Attachment) validate() error {
	if len(a.Body) == 0 || len(a.Body) > maxAttachmentBytes {
		return errors.New("model attachment exceeds byte budget or is empty")
	}
	if len(a.ID)+len(a.Filename)+len(a.MIME) > maxAttachmentMetadata {
		return errors.New("model attachment metadata exceeds budget")
	}
	var config image.Config
	var err error
	switch a.MIME {
	case "image/png":
		config, err = png.DecodeConfig(bytes.NewReader(a.Body))
	case jpegMIME:
		config, err = jpeg.DecodeConfig(bytes.NewReader(a.Body))
	default:
		return errors.New("model attachment format unsupported")
	}
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return errors.New("model attachment is not a readable image")
	}
	if config.Width > maxAttachmentPixels/config.Height {
		return errors.New("model attachment exceeds pixel budget")
	}
	// Header inspection bounds allocation before a full decode catches truncated
	// pixel data. Source bytes are never replaced by the decoded image.
	switch a.MIME {
	case "image/png":
		_, err = png.Decode(bytes.NewReader(a.Body))
	case jpegMIME:
		_, err = jpeg.Decode(bytes.NewReader(a.Body))
	}
	if err != nil {
		return errors.New("model attachment is not a readable image")
	}
	return nil
}

func remoteInput(in Input) ([]byte, error) {
	text, err := boundedInput(in)
	if err != nil {
		return nil, err
	}
	var bounded Input
	if err = json.Unmarshal(text, &bounded); err != nil {
		return nil, errors.New("invalid model input")
	}
	bounded.Attachment = in.Attachment
	bounded.LineupSource = in.LineupSource
	bounded.Frames = in.Frames
	return json.Marshal(bounded)
}

func validateFrames(in Input) error {
	if len(in.Frames) > maxEvidenceFrames || (len(in.Frames) > 0 && in.Attachment != nil) {
		return errors.New("invalid model frame collection")
	}
	total := 0
	for _, frame := range in.Frames {
		total += len(frame.Body)
		if total > maxAttachmentBytes || frame.TimestampMS < 0 || frame.TimestampMS > 240000 {
			return errors.New("model frame budget exceeded")
		}
		if err := frame.validate(); err != nil {
			return err
		}
	}
	return nil
}

// Only the private model route accepts the larger binary envelope.
func decodeInput(w http.ResponseWriter, r *http.Request, in *Input) error {
	limit := int64(base64.StdEncoding.EncodedLen(maxAttachmentBytes) + maxAVInputBytes + maxAttachmentMetadata)
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(in); err != nil {
		return errors.New("invalid model input")
	}
	var tail any
	if !errors.Is(decoder.Decode(&tail), io.EOF) {
		return errors.New("trailing model input")
	}
	_, err := boundedInput(*in)
	return err
}
