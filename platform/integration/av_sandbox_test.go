package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestSandboxAVKindsAndMetadata(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for _, kind := range []telegram.AVKind{telegram.AudioKind, telegram.VoiceKind, telegram.VideoKind, telegram.VideoNoteKind} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			// Invalid media is intentional: the stand transports bytes; the worker validates them.
			body := []byte("synthetic invalid media")
			response := sandboxMediaRequest(t, f,
				"/lab/"+string(kind)+"?user=101&filename=fixture.bin&caption=question&duration=241", body)
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			var update telegram.Update
			require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
			require.NotNil(t, update.Message)
			assert.Nil(t, update.Message.Document)
			assert.Equal(t, "question", update.Message.Caption)
			attachment, present, err := telegram.SelectAV(*update.Message)
			require.NoError(t, err)
			require.True(t, present)
			assert.Equal(t, kind, attachment.Kind)
			assert.Equal(t, 241, attachment.Duration)
			payload, err := f.b.TG.Download(t.Context(), attachment.Document)
			require.NoError(t, err)
			assert.Equal(t, body, payload)
			for _, user := range []string{"101", "202"} {
				request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet,
					f.fake.URL+"/lab/files/"+attachment.Document.FileID+"?user="+user, nil)
				require.NoError(t, requestErr)
				got, requestErr := f.fake.Client().Do(request)
				require.NoError(t, requestErr)
				data, readErr := io.ReadAll(got.Body)
				require.NoError(t, got.Body.Close())
				require.NoError(t, readErr)
				if user == "101" {
					assert.Equal(t, http.StatusOK, got.StatusCode)
					assert.Equal(t, body, data)
				} else {
					assert.Equal(t, http.StatusNotFound, got.StatusCode)
				}
			}
		})
	}
	for _, duration := range []string{"-1", "nan", "86401"} {
		response := sandboxMediaRequest(t, f,
			"/lab/video?user=101&filename=fixture.bin&duration="+duration, []byte("invalid"))
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	}
}

func TestSandboxAVPreviewRangesRemainOwnerBound(t *testing.T) {
	t.Parallel()
	f := setup(t)
	wav, err := os.ReadFile("../testdata/media/orders-en.wav")
	require.NoError(t, err)
	response := sandboxMediaRequest(t, f, "/lab/voice?user=101&filename=voice.wav", wav)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	for _, user := range []string{"101", "202"} {
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet,
			f.fake.URL+"/lab/files/"+update.Message.Voice.FileID+"?preview=1&user="+user, nil)
		require.NoError(t, requestErr)
		request.Header.Set("Range", "bytes=0-3")
		got, requestErr := f.fake.Client().Do(request)
		require.NoError(t, requestErr)
		body, readErr := io.ReadAll(got.Body)
		require.NoError(t, got.Body.Close())
		require.NoError(t, readErr)
		if user == "101" {
			assert.Equal(t, http.StatusPartialContent, got.StatusCode)
			assert.Equal(t, "RIFF", string(body))
			assert.Equal(t, "audio/wave", got.Header.Get("Content-Type"))
			assert.Equal(t, "inline", got.Header.Get("Content-Disposition"))
		} else {
			assert.Equal(t, http.StatusNotFound, got.StatusCode)
		}
	}
}
