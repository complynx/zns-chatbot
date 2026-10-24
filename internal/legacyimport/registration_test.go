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

func registrationArchive(t *testing.T, extra string) ([]byte, string, legacyimport.Record) {
	t.Helper()
	raw, hash := projectedBundle(
		t,
		map[string]string{"users": `{"_id":"u","bot_id":123,"user_id":42,"first_name":"Alice",` + extra + "}\n"},
	)
	var target legacyimport.Record
	_, err := legacyimport.VisitBundle(
		t.Context(),
		bytes.NewReader(raw),
		hash,
		func(record legacyimport.Record) error { target = record; return nil },
	)
	require.NoError(t, err)
	return raw, hash, target
}

func TestEmbeddedRegistrationReadPreservesOldProjectionAndOnlyExposesWhitelistedClaims(t *testing.T) {
	t.Parallel()
	raw, hash, target := registrationArchive(
		t,
		"\"old.event\":{\"state\":\"paid\",\"type\":\"solo\",\"role\":\"leader\",\"paid\":f"+
			"alse,\"bank\":\"PRIVATE_BANK\",\"proof_file\":\"PRIVATE_FILE\",\"couple\":999,\"zi"+
			"tadel_subject\":\"PRIVATE_IAM\"},\"old\":{\"event\":{\"paid\":true}}",
	)
	original := append([]byte(nil), raw...)
	result, validation, err := legacyimport.ReadRegistration(
		t.Context(),
		bytes.NewReader(raw),
		hash,
		target,
		"old.event",
	)
	require.NoError(t, err)
	require.True(t, validation.Inspection.ValidationPassed)
	require.Equal(t, legacyimport.RegistrationProjectionVersion, result.ProjectionVersion)
	require.Equal(t, "claim", result.Status)
	require.Equal(t, "old.event", result.PassKey)
	require.NotNil(t, result.Fields)
	require.Equal(t, "paid", result.Fields.State)
	require.Equal(t, "solo", result.Fields.Type)
	require.Equal(t, "leader", result.Fields.Role)
	require.NotNil(t, result.Fields.PaidClaim)
	require.False(t, *result.Fields.PaidClaim)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_")
	require.NotContains(t, string(encoded), "couple")
	require.Equal(t, original, raw)
	var projection json.RawMessage
	_, err = legacyimport.VisitBundle(
		t.Context(),
		bytes.NewReader(raw),
		hash,
		func(record legacyimport.Record) error { projection = record.Projection; return nil },
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"profile","fields":{"first_name":"Alice"}}`, string(projection))
}

func TestEmbeddedRegistrationReadDistinguishesMissingUnsupportedAndAbsentPaidClaim(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, extra, status string }{
		{"missing", `"other":{"paid":true}`, "missing"},
		{"null", `"event":null`, "unsupported"},
		{"scalar", `"event":"paid"`, "unsupported"},
		{"empty", `"event":{}`, "unsupported"},
		{"unknown-fields", `"event":{"secret":"PRIVATE"}`, "unsupported"},
		{"wrong-paid", `"event":{"state":"paid","paid":"true"}`, "unsupported"},
		{"wrong-role", `"event":{"paid":true,"role":123}`, "unsupported"},
		{"empty-state", `"event":{"state":""}`, "unsupported"},
		{"long-state", `"event":{"state":"` + strings.Repeat("x", 257) + `"}`, "unsupported"},
		{"nul-state", `"event":{"state":"bad\u0000value"}`, "unsupported"},
		{"no-paid", `"event":{"state":"assigned"}`, "claim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, hash, target := registrationArchive(t, tc.extra)
			result, _, err := legacyimport.ReadRegistration(t.Context(), bytes.NewReader(raw), hash, target, "event")
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Status)
			if tc.status == "claim" {
				require.NotNil(t, result.Fields)
				require.Nil(t, result.Fields.PaidClaim)
			} else {
				require.Nil(t, result.Fields)
			}
		})
	}
}

func TestEmbeddedRegistrationReadNeverReturnsProvisionalContent(t *testing.T) {
	t.Parallel()
	raw, hash, target := registrationArchive(t, `"event":{"paid":true}`)
	for _, name := range []string{"corruption", "hash", "owner", "line", "identity", "role", "cancelled", "invalid-key"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := append([]byte(nil), raw...)
			selected := target
			key := "event"
			expected := hash
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch name {
			case "corruption":
				input[len(input)-8] ^= 1
				expected = digest(input)
			case "hash":
				selected.SHA256[0] ^= 1
			case "owner":
				selected.OwnerID++
			case "line":
				selected.Line++
			case "identity":
				selected.SourceKey = "other"
			case "role":
				selected.Role = "messages"
			case "cancelled":
				cancel()
			case "invalid-key":
				key = "bad\x00key"
			}
			result, _, err := legacyimport.ReadRegistration(ctx, bytes.NewReader(input), expected, selected, key)
			require.Error(t, err)
			require.Equal(t, legacyimport.RegistrationClaim{}, result)
			if name == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}
