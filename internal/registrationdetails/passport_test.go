package registrationdetails_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestPassportFieldsValidateImagesAndCountries(t *testing.T) {
	t.Parallel()
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	encoded := base64.StdEncoding.EncodeToString(pngBytes.Bytes())
	mutation := registrationdetails.Mutation{
		OperationID: uuid.New(),
		Field:       registrationdetails.PassportPhoto,
		Value:       encoded,
	}
	record, err := (registrationdetails.Record{}).Apply(mutation)
	require.NoError(t, err)
	require.True(t, record.Status(1).PassportPhotoPresent)
	for _, bad := range []string{encoded[:20], base64.StdEncoding.EncodeToString([]byte("hello")), "file-id"} {
		mutation.Value = bad
		_, err = mutation.Normalize()
		require.ErrorIs(t, err, registrationdetails.ErrInvalid)
	}
	mutation.Field = registrationdetails.PassportCountry
	mutation.Value = " by "
	record, err = record.Apply(mutation)
	require.NoError(t, err)
	require.Equal(t, "BY", record.PassportCountry)
	rules := registrationdetails.Requirements{PassportCountries: []string{"RU", "BY"}, PassportPhoto: true}
	require.Equal(t, []registrationdetails.Field{registrationdetails.LegalName}, rules.Missing(record.Status(2)))
	mutation.Value = "EU"
	_, err = mutation.Normalize()
	require.ErrorIs(t, err, registrationdetails.ErrInvalid)
}
