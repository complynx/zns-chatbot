package legacyimport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func TestRegistrationDiscoveryPagesLiteralKeysAndOmitsUnsupportedData(t *testing.T) {
	t.Parallel()
	raw, hash, target := registrationArchive(t,
		`"Я":{"paid":true},"old.event":{"state":"paid","paid":false,"bank":"PRIVATE_BANK","couple":"PRIVATE_PARTNER"},`+
			`"a":{"role":"admin"},"unsupported-secret":{"paid":"PRIVATE_PROOF"},"scalar-secret":"PRIVATE_SCALAR",`+
			`"nested":{"event":{"paid":true}},"unrelated":{"secret":"PRIVATE_IAM"},"bad\u0000key":{"paid":true}`)
	var keys []string
	after := ""
	for {
		page, validation, err := legacyimport.ReadRegistrations(
			t.Context(),
			bytes.NewReader(raw),
			hash,
			target,
			after,
			1,
		)
		require.NoError(t, err)
		require.True(t, validation.Inspection.ValidationPassed)
		require.Equal(t, "embedded_registration_candidates", page.Kind)
		require.Len(t, page.Items, 1)
		claim := page.Items[0]
		require.Equal(t, "claim", claim.Status)
		keys = append(keys, claim.PassKey)
		if claim.PassKey == "old.event" {
			require.NotNil(t, claim.Fields.PaidClaim)
			require.False(t, *claim.Fields.PaidClaim)
		}
		if claim.PassKey == "a" {
			require.Equal(t, "admin", claim.Fields.Role, "role-only objects are candidates, not verified registrations")
			require.Nil(t, claim.Fields.PaidClaim)
		}
		encoded, err := json.Marshal(page)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "PRIVATE_")
		require.NotContains(t, string(encoded), "secret")
		if !page.HasMore {
			require.Empty(t, page.NextKey)
			break
		}
		require.Equal(t, claim.PassKey, page.NextKey)
		after = page.NextKey
	}
	require.Equal(t, []string{"a", "old.event", "Я"}, keys)
	page, _, err := legacyimport.ReadRegistrations(t.Context(), bytes.NewReader(raw), hash, target, "b", 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.False(t, page.HasMore)
	page, _, err = legacyimport.ReadRegistrations(t.Context(), bytes.NewReader(raw), hash, target, "Я", 20)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.NotNil(t, page.Items)
	require.False(t, page.HasMore)
}

func TestRegistrationDiscoveryFailsClosedOnLimitsAndLateSourceErrors(t *testing.T) {
	t.Parallel()
	raw, hash, target := registrationArchive(t, `"event":{"paid":true}`)
	for _, mode := range []string{"cursor", "limit", "cancelled", "owner", "late-read", "corruption"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			selected := target
			input := io.Reader(bytes.NewReader(raw))
			after, limit := "", 10
			switch mode {
			case "cursor":
				after = "bad\x00key"
			case "limit":
				limit = 21
			case "cancelled":
				cancel()
			case "owner":
				selected.OwnerID++
			case "late-read":
				input = io.MultiReader(
					bytes.NewReader(raw),
					iotest.ErrReader(errors.New("synthetic source read failure")),
				)
			case "corruption":
				changed := append([]byte(nil), raw...)
				changed[len(changed)-8] ^= 1
				input = bytes.NewReader(changed)
			}
			page, _, err := legacyimport.ReadRegistrations(ctx, input, hash, selected, after, limit)
			require.Error(t, err)
			require.Equal(t, legacyimport.RegistrationPage{}, page)
		})
	}
	fields := make(map[string]any, legacyimport.MaxRegistrationFields)
	for i := range legacyimport.MaxRegistrationFields {
		fields[strconv.Itoa(i)] = map[string]any{"paid": true}
	}
	extra, err := json.Marshal(fields)
	require.NoError(t, err)
	raw, hash, target = registrationArchive(t, strings.TrimSuffix(strings.TrimPrefix(string(extra), "{"), "}"))
	page, _, err := legacyimport.ReadRegistrations(t.Context(), bytes.NewReader(raw), hash, target, "", 1)
	require.ErrorIs(t, err, legacyimport.ErrRegistrationScanLimit)
	require.Equal(t, legacyimport.RegistrationPage{}, page)
}
