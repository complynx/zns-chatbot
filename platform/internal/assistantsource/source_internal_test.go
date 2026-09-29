package assistantsource

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestQAFullOrderedUnicodeAndMarkdown(t *testing.T) {
	t.Parallel()
	entries, err := ParseQA(
		[]byte(
			"collection:\n- question: Кто?\n  alt_questions: [Who?, Another?]\n  answer: Ответ🌍\n- answer: Tail\n- {}\n",
		),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"Q: Кто?\n / Who? / Another?\nA: Ответ🌍\n", "A: Tail\n", "\n"}, entries)
	for _, invalid := range [][]byte{[]byte("collection: ["), []byte("collection: []\n---\ncollection: []"), {255}, []byte("{}"), []byte("collection: [{answer: hi, unexpected: value}]")} {
		_, err = ParseQA(invalid)
		require.ErrorIs(t, err, ErrInvalid)
	}
	assert.Equal(t, "*hi* `\\*code\\*` !done", UnescapeMarkdown("\\*hi\\* `\\*code\\*` \\!done"))
	assert.Equal(t, "\\<preserved >unescaped", UnescapeMarkdown("\\<preserved \\>unescaped"))
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testSettings(t *testing.T) config.AssistantSources {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encoded, err := json.Marshal(
		map[string]string{
			"type":         "service_account",
			"client_email": "synthetic@example.invalid",
			"private_key": string(
				pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
			),
			"token_uri": "https://oauth2.googleapis.com/token",
		},
	)
	require.NoError(t, err)
	return config.AssistantSources{
		AboutDocument: "synthetic-document",
		Credentials:   config.Secret(base64.StdEncoding.EncodeToString(encoded)),
	}
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestDriveLibraryOAuthAndBoundedRefresh(t *testing.T) {
	t.Parallel()
	settings := testSettings(t)
	tokens, exports := 0, 0
	mode := "ok"
	scope := ""
	transport := transportFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "oauth2.googleapis.com" {
			tokens++
			var scopeErr error
			scope, scopeErr = assertionScope(request)
			if scopeErr != nil {
				return nil, scopeErr
			}
			return response(
				request,
				http.StatusOK,
				`{"access_token":"synthetic-token","token_type":"Bearer","expires_in":3600}`,
			), nil
		}
		exports++
		if request.Header.Get("Authorization") != "Bearer synthetic-token" ||
			request.URL.Query().Get("mimeType") != "text/markdown" {
			return response(request, http.StatusUnauthorized, "canary"), nil
		}
		switch mode {
		case "oversized":
			return response(request, http.StatusOK, strings.Repeat("x", knowledge.MaxSourceBytes+1)), nil
		case "invalid":
			return response(request, http.StatusOK, string([]byte{255})), nil
		case "error":
			return response(request, http.StatusForbidden, "credential-and-content-canary"), nil
		case "timeout":
			<-request.Context().Done()
			return nil, request.Context().Err()
		default:
			return response(request, http.StatusOK, "full **Unicode🌍**"), nil
		}
	})
	drive, err := NewDrive(settings, transport)
	require.NoError(t, err)
	for range 2 {
		data, fetchErr := drive.Fetch(t.Context())
		require.NoError(t, fetchErr)
		assert.Equal(t, "full **Unicode🌍**", string(data))
	}
	assert.Equal(t, 2, tokens)
	assert.Equal(t, 2, exports)
	assert.Equal(t, driveScope, scope)
	for _, scenario := range []struct {
		mode string
		want error
	}{{"oversized", ErrLimit}, {"invalid", ErrInvalid}, {"error", ErrFetch}} {
		mode = scenario.mode
		_, err = drive.Fetch(t.Context())
		require.ErrorIs(t, err, scenario.want)
		assert.NotContains(t, err.Error(), "canary")
	}
	mode = "timeout"
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = drive.Fetch(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	ctx, cancelNow := context.WithCancel(t.Context())
	cancelNow()
	_, err = drive.Fetch(ctx)
	require.ErrorIs(t, err, context.Canceled)
	mode = "ok"
	_, err = drive.Fetch(t.Context())
	require.NoError(t, err)
	settings.AboutDocument = "different"
	other, err := NewDrive(settings, transport)
	require.NoError(t, err)
	assert.NotEqual(t, drive.Identity(), other.Identity())
}

func assertionScope(request *http.Request) (string, error) {
	if err := request.ParseForm(); err != nil {
		return "", err
	}
	parts := strings.Split(request.Form.Get("assertion"), ".")
	if len(parts) != 3 {
		return "", ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Scope string `json:"scope"`
	}
	if err = json.Unmarshal(data, &claims); err != nil {
		return "", err
	}
	return claims.Scope, nil
}

func TestSourceReaderLimits(t *testing.T) {
	t.Parallel()
	_, err := readBounded(bytes.NewReader(bytes.Repeat([]byte{'a'}, knowledge.MaxSourceBytes+1)))
	require.ErrorIs(t, err, ErrLimit)
	_, err = NewDrive(config.AssistantSources{Credentials: "not-base64", AboutDocument: "id"}, nil)
	require.ErrorIs(t, err, ErrInvalid)
}
