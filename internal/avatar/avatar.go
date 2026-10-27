// Package avatar defines the bounded protocol for the isolated avatar renderer.
package avatar

import (
	"context"
	"errors"
)

const (
	ExitInvalidImage = 2
	ExitImageLimit   = 3
	MaxInputBytes    = 20 * 1024 * 1024
	MaxOutputBytes   = 4 * 1024 * 1024
	MaxPixels        = 8_000_000
	MaxDimension     = 8192
	OutputSize       = 1024
	ProcessorVersion = "zinusya-avatar-v1"
	FrameSHA256      = "fd96f42262a184ac26bae5c7a623789dd93a5724b8da7fe2f05cce5a7976a786"
)

var (
	ErrInvalidTransform     = errors.New("invalid avatar transform")
	ErrInvalidImage         = errors.New("invalid or unsupported avatar image")
	ErrImageLimit           = errors.New("avatar image exceeds processing limits")
	ErrProcessorUnavailable = errors.New("avatar processor unavailable")
)

type Rendered struct {
	Transform        *Transform `json:"transform,omitempty"`
	JPEG             []byte     `json:"jpeg"`
	FrameSHA256      string     `json:"frame_sha256"`
	ProcessorVersion string     `json:"processor_version"`
	SourceSHA256     string     `json:"source_sha256"`
	SHA256           string     `json:"sha256"`
	Width            int        `json:"width"`
	Height           int        `json:"height"`
}

type Processor interface {
	Render(context.Context, []byte, Transform) (Rendered, error)
}
