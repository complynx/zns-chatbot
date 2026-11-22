package registrationdetails

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg" // Register JPEG for image.Decode.
	_ "image/png"  // Register PNG for image.Decode.

	"golang.org/x/text/language"
)

const (
	MaxPhotoBytes = 5 << 20
	Alpha2Length  = 2
)

func validCountry(code string) bool {
	if len(code) != Alpha2Length {
		return false
	}
	region, err := language.ParseRegion(code)
	return err == nil && region.IsCountry() && region.String() == code
}

func validPhoto(encoded string) bool {
	if len(encoded) == 0 || len(encoded) > base64.StdEncoding.EncodedLen(MaxPhotoBytes) {
		return false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > MaxPhotoBytes {
		return false
	}
	// Bound decoded pixel memory before inspecting all of the image.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > 24_000_000 {
		return false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return err == nil && img.Bounds().Dx() == cfg.Width && img.Bounds().Dy() == cfg.Height
}
