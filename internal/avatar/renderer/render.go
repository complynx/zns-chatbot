// Package renderer is linked only into the leaf image-processing executable.
package renderer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // Register PNG decoding for bounded image input and the embedded frame.

	"github.com/disintegration/imaging"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/frame"
)

const jpegQuality = 100
const centerDivisor = 2

func Render(ctx context.Context, raw []byte, transform avatar.Transform) (avatar.Rendered, error) {
	if err := ctx.Err(); err != nil {
		return avatar.Rendered{}, err
	}
	transform, err := avatar.NormalizeTransform(&transform)
	if err != nil {
		return avatar.Rendered{}, err
	}
	if len(raw) > avatar.MaxInputBytes {
		return avatar.Rendered{}, avatar.ErrImageLimit
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") {
		return avatar.Rendered{}, avatar.ErrInvalidImage
	}
	if config.Width < 1 || config.Height < 1 || config.Width > avatar.MaxDimension ||
		config.Height > avatar.MaxDimension ||
		int64(config.Width)*int64(config.Height) > avatar.MaxPixels {
		return avatar.Rendered{}, avatar.ErrImageLimit
	}
	photo, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return avatar.Rendered{}, avatar.ErrInvalidImage
	}
	overlay, err := imaging.Decode(frame.Reader())
	if err != nil {
		return avatar.Rendered{}, avatar.ErrProcessorUnavailable
	}
	if err = ctx.Err(); err != nil {
		return avatar.Rendered{}, err
	}
	var applied *avatar.Transform
	version := avatar.ProcessorVersion
	if transform == avatar.IdentityTransform() {
		photo = centerSquare(photo)
	} else {
		photo, err = transformPhoto(ctx, photo, transform)
		if err != nil {
			return avatar.Rendered{}, err
		}
		applied, version = &transform, avatar.EditedProcessorVersion
	}
	overlay = centerSquare(overlay)
	canvas := image.NewRGBA(image.Rect(0, 0, avatar.OutputSize, avatar.OutputSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), photo, photo.Bounds().Min, draw.Over)
	draw.Draw(canvas, canvas.Bounds(), overlay, overlay.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err = jpeg.Encode(&output, canvas, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return avatar.Rendered{}, avatar.ErrProcessorUnavailable
	}
	if output.Len() > avatar.MaxOutputBytes {
		return avatar.Rendered{}, avatar.ErrImageLimit
	}
	if err = ctx.Err(); err != nil {
		return avatar.Rendered{}, err
	}
	sourceHash, outputHash := sha256.Sum256(raw), sha256.Sum256(output.Bytes())
	return avatar.Rendered{
		JPEG: output.Bytes(), Width: avatar.OutputSize, Height: avatar.OutputSize,
		SourceSHA256: hex.EncodeToString(sourceHash[:]), SHA256: hex.EncodeToString(outputHash[:]),
		FrameSHA256: frame.SHA256(), ProcessorVersion: version, Transform: applied,
	}, nil
}

func centerSquare(src image.Image) image.Image {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	left, top := b.Min.X+(b.Dx()-side)/centerDivisor, b.Min.Y+(b.Dy()-side)/centerDivisor
	cropped := imaging.Crop(src, image.Rect(left, top, left+side, top+side))
	return imaging.Resize(cropped, avatar.OutputSize, avatar.OutputSize, imaging.Lanczos)
}
