package renderer

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/disintegration/imaging"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

const pixelCenter = 0.5
const channelMax = 65535
const channelScale = 257
const opaqueAlpha = 255

// transformPhoto samples the fixed output square directly. Zoom and source
// aspect ratio never enlarge an intermediate image beyond the bounded source.
func transformPhoto(ctx context.Context, src image.Image, t avatar.Transform) (image.Image, error) {
	dst := image.NewRGBA(image.Rect(0, 0, avatar.OutputSize, avatar.OutputSize))
	bounds := src.Bounds()
	scale := float64(
		avatar.OutputSize,
	) / float64(
		min(bounds.Dx(), bounds.Dy()),
	) * float64(
		t.ZoomPPM,
	) / float64(
		avatar.FramePPM,
	)
	// Prefilter shrinking inputs so bilinear affine sampling does not alias detail.
	sampled := src
	if scale < 1 {
		sampled = imaging.Resize(
			src,
			int(math.Ceil(float64(bounds.Dx())*scale)),
			int(math.Ceil(float64(bounds.Dy())*scale)),
			imaging.Lanczos,
		)
	}
	sampleBounds := sampled.Bounds()
	sampleScaleX := float64(sampleBounds.Dx()) / float64(bounds.Dx())
	sampleScaleY := float64(sampleBounds.Dy()) / float64(bounds.Dy())
	angle := float64(t.AngleMilli) * math.Pi / float64(avatar.HalfTurnMilli)
	sin, cos := math.Sincos(angle)
	center := float64(avatar.OutputSize) / centerDivisor
	offsetX := float64(t.OffsetXPPM) / float64(avatar.FramePPM) * float64(avatar.OutputSize)
	offsetY := float64(t.OffsetYPPM) / float64(avatar.FramePPM) * float64(avatar.OutputSize)
	sourceX := float64(bounds.Min.X) + float64(bounds.Dx())/centerDivisor - pixelCenter
	sourceY := float64(bounds.Min.Y) + float64(bounds.Dy())/centerDivisor - pixelCenter
	for y := range avatar.OutputSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := range avatar.OutputSize {
			dx, dy := float64(x)+pixelCenter-center-offsetX, float64(y)+pixelCenter-center-offsetY
			sx, sy := (cos*dx+sin*dy)/scale+sourceX, (-sin*dx+cos*dy)/scale+sourceY
			sx = (sx-float64(bounds.Min.X)+pixelCenter)*sampleScaleX + float64(sampleBounds.Min.X) - pixelCenter
			sy = (sy-float64(bounds.Min.Y)+pixelCenter)*sampleScaleY + float64(sampleBounds.Min.Y) - pixelCenter
			dst.SetRGBA(x, y, sampleWhite(sampled, sx, sy))
		}
	}
	return dst, nil
}

func sampleWhite(src image.Image, x, y float64) color.RGBA {
	left, top := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(left), y-float64(top)
	pixels := [4]color.RGBA64{
		whitePixel(src, left, top),
		whitePixel(src, left+1, top),
		whitePixel(src, left, top+1),
		whitePixel(src, left+1, top+1),
	}
	weights := [4]float64{(1 - fx) * (1 - fy), fx * (1 - fy), (1 - fx) * fy, fx * fy}
	var r, g, b float64
	for i, pixel := range pixels {
		r += float64(pixel.R) * weights[i]
		g += float64(pixel.G) * weights[i]
		b += float64(pixel.B) * weights[i]
	}
	channel := func(value float64) uint8 {
		return uint8(math.Round(min(float64(channelMax), max(0, value)) / channelScale))
	}
	return color.RGBA{R: channel(r), G: channel(g), B: channel(b), A: opaqueAlpha}
}

func whitePixel(src image.Image, x, y int) color.RGBA64 {
	if !image.Pt(x, y).In(src.Bounds()) {
		return color.RGBA64{R: channelMax, G: channelMax, B: channelMax, A: channelMax}
	}
	r, g, b, a := src.At(x, y).RGBA()
	white := uint32(channelMax) - a
	return color.RGBA64{
		R: uint16(min(r+white, uint32(channelMax))),
		G: uint16(min(g+white, uint32(channelMax))),
		B: uint16(min(b+white, uint32(channelMax))),
		A: channelMax,
	}
}
