package symboldescribe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type fakeTelegram struct{ raw []byte }

func (f fakeTelegram) CustomEmojiStickers(context.Context, []string) ([]telegram.Sticker, error) {
	return []telegram.Sticker{{FileID: "private-target", UniqueID: "u", CustomEmojiID: "1", SetName: "set"}}, nil
}
func (f fakeTelegram) StickerSet(context.Context, string) (telegram.StickerSet, error) {
	return telegram.StickerSet{Name: "set", Stickers: []telegram.Sticker{
		{FileID: "a", UniqueID: "u", CustomEmojiID: "1"}, {FileID: "b", UniqueID: "v", CustomEmojiID: "2"},
	}}, nil
}
func (f fakeTelegram) ReadFile(context.Context, string, int64) ([]byte, error) { return f.raw, nil }

type fakeLoom struct {
	chats, uploads, creates, reads int
	uploadMIMEs                    []string
	uploadBytes                    [][]byte
	uploadError                    error
	createError                    error
	request                        loom.CreateRunRequest
	status                         string
	result                         string
}

func (f *fakeLoom) CreateChat(context.Context, loom.Auth) (string, error) {
	f.chats++
	return "isolated-chat", nil
}
func (f *fakeLoom) UploadArtifact(_ context.Context, _ loom.Auth, mime string, raw []byte) (string, error) {
	f.uploads++
	f.uploadMIMEs = append(f.uploadMIMEs, mime)
	f.uploadBytes = append(f.uploadBytes, bytes.Clone(raw))
	return uuid.NewString(), f.uploadError
}
func (f *fakeLoom) CreateRun(_ context.Context, _ loom.Auth, _ string, r loom.CreateRunRequest) (loom.Run, error) {
	f.creates++
	f.request = r
	return loom.Run{ID: "run", Status: "queued"}, f.createError
}
func (f *fakeLoom) GetRun(context.Context, loom.Auth, string) (loom.Run, error) {
	f.reads++
	return loom.Run{ID: "run", Status: f.status, Result: f.result}, nil
}

func fixture(t *testing.T) (symboldescribe.Flow, *fakeLoom, *symboldescribe.State) {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8))))
	lc := &fakeLoom{status: "running"}
	return symboldescribe.Flow{Telegram: fakeTelegram{b.Bytes()}, Loom: lc}, lc,
		&symboldescribe.State{Generation: uuid.New(), Reference: symbols.Reference{Kind: symbols.Emoji, ID: "1"}}
}

func TestDescriptionFlowIsolatedSetContextAndResume(t *testing.T) {
	t.Parallel()
	f, lc, s := fixture(t)
	var snapshots []symboldescribe.State
	save := func(v symboldescribe.State) error { snapshots = append(snapshots, v); return nil }
	require.NoError(t, f.Step(t.Context(), loom.Auth{ServiceToken: "actor", SessionID: "descriptor"}, s, save))
	require.Equal(t, "running", s.Phase)
	require.Equal(t, "isolated-chat", lc.request.ChatID)
	require.Empty(t, lc.request.Input.ArtifactIDs)
	for _, id := range s.ArtifactIDs {
		require.Contains(t, lc.request.Input.Text, id)
	}
	require.Contains(t, lc.request.Input.Text, "Do not attach an animated original")
	require.Contains(t, lc.request.Input.Text, "ornament")
	require.Contains(t, lc.request.Input.Text, "target_position_in_contact_sheet\":1")
	require.NotContains(t, lc.request.Input.Text, "private-target")
	lc.status = "completed"
	lc.result = `{"description":"Left end of a bone","category":"fragment","composition":"Joins right end"}`
	require.NoError(t, f.Step(t.Context(), loom.Auth{}, s, save))
	require.Equal(t, "done", s.Phase)
	require.Equal(t, "fragment", s.Result.Category)
	require.Equal(t, 1, lc.chats)
	require.Equal(t, 2, lc.uploads)
	require.Equal(t, 1, lc.creates)
	require.NotEmpty(t, snapshots)
}

func TestDescriptionUnknownUploadNeverReplays(t *testing.T) {
	t.Parallel()
	f, lc, s := fixture(t)
	lc.uploadError = errors.New("lost receipt")
	save := func(symboldescribe.State) error { return nil }
	require.Error(t, f.Step(t.Context(), loom.Auth{}, s, save))
	require.Equal(t, "upload_artifact", s.Pending)
	require.Error(t, f.Step(t.Context(), loom.Auth{}, s, save))
	require.Equal(t, 1, lc.chats)
	require.Equal(t, 1, lc.uploads)
	require.Zero(t, lc.creates)
}

func TestDescriptionResumesAcknowledgedChatAndUploads(t *testing.T) {
	t.Parallel()
	for _, uploaded := range []int{0, 1} {
		f, lc, s := fixture(t)
		var persisted []byte
		interrupted := false
		save := func(v symboldescribe.State) error {
			var err error
			persisted, err = json.Marshal(v)
			if err != nil {
				return err
			}
			if !interrupted && v.Phase == "uploading" && v.Pending == "" && len(v.ArtifactIDs) == uploaded {
				interrupted = true
				return errors.New("process interrupted after durable checkpoint")
			}
			return nil
		}
		require.Error(t, f.Step(t.Context(), loom.Auth{}, s, save))
		require.True(t, interrupted)
		var restored symboldescribe.State
		require.NoError(t, json.Unmarshal(persisted, &restored))
		require.NoError(t, f.Step(t.Context(), loom.Auth{}, &restored, save))
		require.Equal(t, "running", restored.Phase)
		require.Equal(t, 1, lc.chats)
		require.Equal(t, 2, lc.uploads)
		require.Equal(t, 1, lc.creates)
	}
}

func TestDescriptionRejectsMalformedOrOverconfidentSchema(t *testing.T) {
	t.Parallel()
	for _, result := range []string{`{"description":"x","category":"business_action"}`,
		`{"description":"x","category":"text","extra":"y"}`, `{"description":"x","category":"text"} {}`} {
		f, lc, s := fixture(t)
		s.Phase = "running"
		s.RunID = "run"
		lc.status = "completed"
		lc.result = result
		require.Error(t, f.Step(t.Context(), loom.Auth{}, s, func(symboldescribe.State) error { return nil }))
		require.Nil(t, s.Result)
	}
}
