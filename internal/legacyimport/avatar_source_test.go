package legacyimport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func avatarInputArchive(t *testing.T, fields string) ([]byte, string, legacyimport.Record) {
	t.Helper()
	raw, hash := projectedBundle(t, map[string]string{
		"users": `{"_id":"u","bot_id":123,"user_id":42}` + "\n",
		"files": `{"_id":"image","bot_id":123,"user_id":42,` + fields + "}\n",
	})
	var selected legacyimport.Record
	_, err := legacyimport.VisitBundle(t.Context(), bytes.NewReader(raw), hash, func(record legacyimport.Record) error {
		if record.Role == "files" {
			selected = record
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "files", selected.Role)
	return raw, hash, selected
}

func TestHistoricalAvatarReferenceKeepsTelegramIDsPrivate(t *testing.T) {
	t.Parallel()
	for _, mime := range []string{"image/jpeg", "image/png"} {
		t.Run(mime, func(t *testing.T) {
			t.Parallel()
			fields := `"tg_file_id":"PRIVATE_FILE_ID","tg_file_unique_id":"PRIVATE_UNIQUE_ID","mime_type":"` + mime + `","status":"completed_simple","cached_file_key":"PRIVATE_PATH","original_file_name":"PRIVATE_NAME","bank":"PRIVATE_BANK"`
			raw, hash, target := avatarInputArchive(t, fields)
			result, validation, err := legacyimport.ReadAvatarInput(t.Context(), bytes.NewReader(raw), hash, target)
			require.NoError(t, err)
			require.True(t, validation.Inspection.ValidationPassed)
			require.Equal(t, "PRIVATE_FILE_ID", result.FileID)
			require.Equal(t, "PRIVATE_UNIQUE_ID", result.UniqueID)
			require.Equal(t, mime, result.MIMEType)
			require.Equal(t, "completed_simple", result.SourceStatus)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "PRIVATE_")
			require.JSONEq(t, `{"mime_type":"`+mime+`","source_status":"completed_simple"}`, string(encoded))
			require.Nil(t, target.Projection, "the established files history projection must remain absent")
		})
	}
}

func TestHistoricalAvatarInputRejectsNonImagesAndMalformedMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, fields string }{
		{"payment-pdf", `"mime_type":"application/pdf","tg_file_id":"file","tg_file_unique_id":"unique"`},
		{"missing-file", `"mime_type":"image/png","tg_file_unique_id":"unique"`},
		{"missing-unique", `"mime_type":"image/png","tg_file_id":"file"`},
		{"empty-file", `"mime_type":"image/png","tg_file_id":"","tg_file_unique_id":"unique"`},
		{"nul-file", `"mime_type":"image/png","tg_file_id":"bad\u0000file","tg_file_unique_id":"unique"`},
		{"long-file", `"mime_type":"image/png","tg_file_id":"` + strings.Repeat("x", 1025) + `","tg_file_unique_id":"unique"`},
		{"numeric-status", `"mime_type":"image/png","tg_file_id":"file","tg_file_unique_id":"unique","status":123`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, hash, target := avatarInputArchive(t, tc.fields)
			result, _, err := legacyimport.ReadAvatarInput(t.Context(), bytes.NewReader(raw), hash, target)
			require.ErrorIs(t, err, legacyimport.ErrAvatarInput)
			require.Equal(t, legacyimport.AvatarInput{}, result)
		})
	}
}

func TestHistoricalAvatarInputNeverReturnsUnverifiedProvisionalMetadata(t *testing.T) {
	t.Parallel()
	raw, hash, target := avatarInputArchive(
		t,
		`"mime_type":"image/jpeg","tg_file_id":"PRIVATE_FILE","tg_file_unique_id":"PRIVATE_UNIQUE"`,
	)
	for _, name := range []string{"late-corruption", "owner", "canonical-hash", "line", "missing-key", "wrong-role", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := append([]byte(nil), raw...)
			selected, expected := target, hash
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch name {
			case "late-corruption":
				input[len(input)-8] ^= 1
				expected = digest(input)
			case "owner":
				selected.OwnerID++
			case "canonical-hash":
				selected.SHA256[0] ^= 1
			case "line":
				selected.Line++
			case "missing-key":
				selected.SourceKey = "str:missing"
			case "wrong-role":
				selected.Role = "users"
			case "cancelled":
				cancel()
			}
			result, validation, err := legacyimport.ReadAvatarInput(ctx, bytes.NewReader(input), expected, selected)
			require.Error(t, err)
			require.Equal(t, legacyimport.AvatarInput{}, result)
			require.False(t, validation.Inspection.ValidationPassed)
		})
	}
}
