package legacyimport

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const maxAvatarFileIDBytes = 1024
const maxAvatarStatusBytes = 128

var ErrAvatarInput = errors.New("unsupported historical avatar input")

// AvatarInput holds a historical Telegram image reference for server-side reads.
// It does not prove current availability or recovery of a finished avatar.
// Telegram identifiers never enter API responses or agent input.
type AvatarInput struct {
	FileID       string `json:"-"`
	UniqueID     string `json:"-"`
	MIMEType     string `json:"mime_type"`
	SourceStatus string `json:"source_status,omitempty"`
}

// ReadAvatarInput selects an exact files record and validates the whole archive
// before returning its bounded metadata. It neither fetches nor stores images.
func ReadAvatarInput(ctx context.Context, input io.Reader, digest string, target Record,
) (AvatarInput, BundleValidation, error) {
	if target.Role != roleFiles {
		return AvatarInput{}, BundleValidation{}, ErrSourceRecord
	}
	var result AvatarInput
	validation, err := readSourceRecord(ctx, input, digest, target, func(object map[string]any) error {
		mime, ok := object["mime_type"].(string)
		if !ok || (mime != "image/jpeg" && mime != "image/png") {
			return ErrAvatarInput
		}
		for _, field := range []struct {
			name  string
			value *string
		}{{"tg_file_id", &result.FileID}, {"tg_file_unique_id", &result.UniqueID}} {
			value, present := object[field.name].(string)
			if !present || !boundedAvatarField(value, maxAvatarFileIDBytes) {
				return ErrAvatarInput
			}
			*field.value = value
		}
		result.MIMEType = mime
		if raw, present := object["status"]; present {
			value, valid := raw.(string)
			if !valid || !boundedAvatarField(value, maxAvatarStatusBytes) {
				return ErrAvatarInput
			}
			result.SourceStatus = value
		}
		return nil
	})
	if err != nil {
		return AvatarInput{}, BundleValidation{}, err
	}
	return result, validation, nil
}

func boundedAvatarField(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
