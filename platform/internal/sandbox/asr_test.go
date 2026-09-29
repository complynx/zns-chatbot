package sandbox_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestFixtureASROnlyRecognizesKnownPCM(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		file, text string
	}{
		{"orders-en", "Please show my orders."},
		{"receipt-second-en", "This receipt is for the second order."},
		{"profile-en", "My full legal name is Taylor Synthetic Example."},
		{"help-ru", "Какие услуги я могу забронировать?"},
	} {
		t.Run(fixture.file, func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile("../../testdata/media/" + fixture.file + ".wav")
			require.NoError(t, err)
			fake := (&sandbox.Fake{Token: "sandbox"}).Handler()
			for _, sample := range []struct {
				name, key, model string
				body             []byte
				status           int
			}{
				{"known", "sandbox-only-synthetic-asr", "gpt-transcribe", body, http.StatusOK},
				{"bad_auth", "wrong", "gpt-transcribe", body, http.StatusUnauthorized},
				{"bad_model", "sandbox-only-synthetic-asr", "unknown", body, http.StatusBadRequest},
				{"truncated", "sandbox-only-synthetic-asr", "gpt-transcribe", body[:20], http.StatusBadRequest},
			} {
				response := fixtureASRRequest(t, fake, sample.key, sample.model, sample.body)
				require.Equal(t, sample.status, response.Code, sample.name)
				if response.Code == http.StatusOK {
					var result map[string]string
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
					assert.Equal(t, fixture.text, result["text"])
				}
			}
			changed := bytes.Clone(body)
			changed[len(changed)-1] ^= 1
			response := fixtureASRRequest(t, fake, "sandbox-only-synthetic-asr", "gpt-transcribe", changed)
			assert.Equal(t, http.StatusUnprocessableEntity, response.Code)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/lab/asr/state", nil)
			request.Header.Set("X-Sandbox", "1")
			state := httptest.NewRecorder()
			fake.ServeHTTP(state, request)
			assert.JSONEq(t, `{"recognized_fixture_calls":1}`, state.Body.String())
		})
	}
}

func fixtureASRRequest(t *testing.T, handler http.Handler, key, model string, wav []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "audio.wav")
	require.NoError(t, err)
	_, err = file.Write(wav)
	require.NoError(t, err)
	require.NoError(t, form.WriteField("model", model))
	require.NoError(t, form.Close())
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/lab/asr", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
