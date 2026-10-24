package miniapp_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/miniapp"
)

const fixtureToken = "6087685431:fixture_only_secret"
const fixtureBot = int64(6087685431)
const fixtureSeconds = int64(1792029600)

// Fixed independently generated Python hmac/urlencode fixture, not real initData.
const fixtureProof = "auth_date=1792029600&user=%7B%22id%22%3A8968148043%2C%22first_name%22%3A" +
	"%22QA%22%2C%22language_code%22%3A%22ru%22%7D&signature=signed-unknown-fi" +
	"xture&start_param=test&hash=48b382a3a1a6c78f13380d5213caade2158bf1b0f8cb" +
	"99e7fa001ae88e635252"

func TestMiniAppVerifiesIndependentFixtureAndCanonicalReplayIdentity(t *testing.T) {
	t.Parallel()
	now := time.Unix(fixtureSeconds, 0).UTC()
	proof, err := miniapp.Verify(fixtureProof, fixtureToken, fixtureBot, now, 5*time.Minute, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, int64(8968148043), proof.User.ID)
	require.Equal(t, "QA", proof.User.FirstName)
	require.Equal(t, "ru", proof.User.LanguageCode)
	require.Equal(t, now, proof.AuthDate)
	require.Equal(
		t,
		"ae88fe119c8f1d564e826b6e14420bb22a9f316189cec0f7532851c4ddf7255a",
		hex.EncodeToString(proof.Digest[:]),
	)
	parts := strings.Split(strings.ReplaceAll(fixtureProof, "%7B", "%7b"), "&")
	slices.Reverse(parts)
	reordered, err := miniapp.Verify(
		strings.Join(parts, "&"),
		fixtureToken,
		fixtureBot,
		now,
		5*time.Minute,
		30*time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, proof.Digest, reordered.Digest)
	_, err = miniapp.Verify(
		strings.Replace(fixtureProof, "signed-unknown-fixture", "tampered", 1),
		fixtureToken,
		fixtureBot,
		now,
		5*time.Minute,
		30*time.Second,
	)
	require.ErrorIs(
		t,
		err,
		miniapp.ErrInvalid,
		"Unknown fields, including signature, participate in this HMAC protocol",
	)
}

func TestMiniAppRejectsTamperingWrongBotAndInvalidFreshness(t *testing.T) {
	t.Parallel()
	now := time.Unix(fixtureSeconds, 0).UTC()
	cases := []struct {
		name, raw, token string
		bot              int64
		at               time.Time
	}{
		{"tampered user", strings.Replace(fixtureProof, "8968148043", "8968148044", 1), fixtureToken, fixtureBot, now},
		{"wrong secret", fixtureProof, "6087685431:other_secret", fixtureBot, now},
		{"wrong deployment", fixtureProof, fixtureToken, fixtureBot + 1, now},
		{"missing token", fixtureProof, "", fixtureBot, now},
		{"expired equality", fixtureProof, fixtureToken, fixtureBot, now.Add(5 * time.Minute)},
		{"future", fixtureProof, fixtureToken, fixtureBot, now.Add(-31 * time.Second)},
		{"duplicate hash", fixtureProof + "&hash=00", fixtureToken, fixtureBot, now},
		{"duplicate user", fixtureProof + "&user=%7B%22id%22%3A1%7D", fixtureToken, fixtureBot, now},
		{"malformed", fixtureProof + "&extra=%ZZ", fixtureToken, fixtureBot, now},
		{"newline field", fixtureProof + "&user%0Aignored=x", fixtureToken, fixtureBot, now},
		{"oversized", strings.Repeat("a", miniapp.MaxInitDataBytes+1), fixtureToken, fixtureBot, now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := miniapp.Verify(tc.raw, tc.token, tc.bot, tc.at, 5*time.Minute, 30*time.Second)
			require.ErrorIs(t, err, miniapp.ErrInvalid)
		})
	}
	_, err := miniapp.Verify(
		fixtureProof,
		fixtureToken,
		fixtureBot,
		now.Add(-30*time.Second),
		5*time.Minute,
		30*time.Second,
	)
	require.NoError(t, err, "Future skew equality is allowed")
	_, err = miniapp.Verify(
		fixtureProof,
		fixtureToken,
		fixtureBot,
		now.Add(5*time.Minute-time.Nanosecond),
		5*time.Minute,
		30*time.Second,
	)
	require.NoError(t, err, "Proof is valid strictly before expiry")
}

func TestMiniAppRejectsSignedAmbiguousAndInvalidUsers(t *testing.T) {
	t.Parallel()
	users := []string{
		`{"id":1,"id":2}`,
		`{"id":1,"\u0069d":2}`,
		`{"id":1,"other":{"a":1,"a":2}}`,
		`{"id":1.5}`,
		`{"id":0}`,
		`{"id":-1}`,
		`{"id":4503599627370496}`,
		`{"id":1,"is_bot":true}`,
		`{"id":"1"}`,
		`{"ID":1}`,
		`null`,
		`[]`,
		`{"id":1} {"id":2}`,
		`{"id":1,"other":` + strings.Repeat("[", 20) + "0" + strings.Repeat("]", 20) + "}",
	}
	for _, user := range users {
		_, err := miniapp.Verify(
			signProof(user),
			fixtureToken,
			fixtureBot,
			time.Unix(fixtureSeconds, 0),
			5*time.Minute,
			30*time.Second,
		)
		require.ErrorIs(t, err, miniapp.ErrInvalid, user)
	}
	proof, err := miniapp.Verify(
		signProof(`{"id":1,"ID":2,"is_bot":false,"first_name":"<untrusted>"}`),
		fixtureToken,
		fixtureBot,
		time.Unix(fixtureSeconds, 0),
		5*time.Minute,
		30*time.Second,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), proof.User.ID, "Only the exact signed Telegram id field controls identity")
}

func signProof(user string) string {
	fields := url.Values{"auth_date": {"1792029600"}, "user": {user}}
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte(fixtureToken))
	mac := hmac.New(sha256.New, key.Sum(nil))
	_, _ = mac.Write([]byte("auth_date=1792029600\nuser=" + user))
	fields.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return fields.Encode()
}
