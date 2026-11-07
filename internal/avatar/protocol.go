package avatar

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const MaxHeaderBytes = 1024
const renderProtocolVersion = 2

// inputFramingBytes allows the header newline and one over-limit sentinel byte.
const inputFramingBytes = 2

type renderHeader struct {
	ProtocolVersion int       `json:"protocol_version"`
	Transform       Transform `json:"transform"`
}

func renderInput(raw []byte, transform Transform) (io.Reader, error) {
	if transform == IdentityTransform() {
		return bytes.NewReader(raw), nil
	}
	header, err := json.Marshal(renderHeader{ProtocolVersion: renderProtocolVersion, Transform: transform})
	if err != nil {
		return nil, err
	}
	header = append(header, '\n')
	return io.MultiReader(bytes.NewReader(header), bytes.NewReader(raw)), nil
}

// ReadInput accepts legacy raw images or a strict bounded v2 JSON header line
// followed by original image bytes. No base64 copy or image library is required.
func ReadInput(reader io.Reader) ([]byte, Transform, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, MaxInputBytes+MaxHeaderBytes+inputFramingBytes))
	if err != nil {
		return nil, Transform{}, err
	}
	transform := IdentityTransform()
	if len(raw) > 0 && raw[0] == '{' {
		end := bytes.IndexByte(raw, '\n')
		if end < 0 || end > MaxHeaderBytes {
			return nil, Transform{}, ErrInvalidTransform
		}
		var header renderHeader
		decoder := json.NewDecoder(bytes.NewReader(raw[:end]))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&header) != nil || header.ProtocolVersion != renderProtocolVersion {
			return nil, Transform{}, ErrInvalidTransform
		}
		var trailing any
		if !errors.Is(decoder.Decode(&trailing), io.EOF) {
			return nil, Transform{}, ErrInvalidTransform
		}
		transform, err = NormalizeTransform(&header.Transform)
		if err != nil {
			return nil, Transform{}, err
		}
		raw = raw[end+1:]
	}
	if len(raw) > MaxInputBytes {
		return nil, Transform{}, ErrImageLimit
	}
	if len(raw) == 0 {
		return nil, Transform{}, ErrInvalidImage
	}
	return raw, transform, nil
}
