package symboldescribe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type fakeTelegram struct{ raw []byte }

type previewFailureTelegram struct {
	fakeTelegram

	failure error
	reads   int
}

func (f *previewFailureTelegram) ReadFile(ctx context.Context, id string, limit int64) ([]byte, error) {
	if id != "private-target" {
		f.reads++
		if f.failure != nil {
			return nil, f.failure
		}
	}
	return f.fakeTelegram.ReadFile(ctx, id, limit)
}

func TestPreviewReadRetriesWithoutReplayingAcknowledgedOriginal(t *testing.T) {
	t.Parallel()
	f, lc, state := fixture(t)
	tg := &previewFailureTelegram{fakeTelegram: f.Telegram.(fakeTelegram),
		failure: &telegram.Error{StatusCode: 502, Outcome: telegram.Rejected}}
	f.Telegram = tg
	save := func(symboldescribe.State) error { return nil }
	require.Error(t, f.Step(t.Context(), loom.Auth{}, state, save))
	require.Equal(t, 3, tg.reads)
	require.Empty(t, state.Pending)
	require.Len(t, state.ArtifactIDs, 1)
	require.Equal(t, 1, lc.uploads)
	tg.failure = nil
	require.NoError(t, f.Step(t.Context(), loom.Auth{}, state, save))
	require.Equal(t, 1, lc.chats)
	require.Equal(t, 2, lc.uploads)
	require.Equal(t, 1, lc.creates)
}

func TestPermanentPreviewFailureIsNotRetried(t *testing.T) {
	t.Parallel()
	f, _, state := fixture(t)
	tg := &previewFailureTelegram{fakeTelegram: f.Telegram.(fakeTelegram), failure: telegram.ErrFileAccessDenied}
	f.Telegram = tg
	require.ErrorIs(t, f.Step(t.Context(), loom.Auth{}, state, func(symboldescribe.State) error { return nil }),
		telegram.ErrFileAccessDenied)
	require.Equal(t, 1, tg.reads)
}

func TestDescriptorTraceLinksFileReadsWithoutSourceIdentifiers(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	f, _, state := fixture(t)
	f.Tracer = provider.Tracer("test")
	require.NoError(t, f.Step(t.Context(), loom.Auth{}, state, func(symboldescribe.State) error { return nil }))
	spans := recorder.Ended()
	require.Len(t, spans, 4)
	root := spans[3]
	require.Equal(t, "symbols.describe_step", root.Name())
	for _, child := range spans[:3] {
		require.Equal(t, "symbols.read_file", child.Name())
		require.Equal(t, root.SpanContext().TraceID(), child.SpanContext().TraceID())
		require.Equal(t, root.SpanContext().SpanID(), child.Parent().SpanID())
		require.Len(t, child.Events(), 1)
		require.Equal(t, "read_attempt", child.Events()[0].Name)
		require.NotContains(t, fmt.Sprint(child.Attributes(), child.Events()), "private-target")
	}
}

func TestDescriptorTraceMarksNonMediaFailureWithoutErrorText(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	f, lc, state := fixture(t)
	f.Tracer = provider.Tracer("test")
	lc.capabilityError = errors.New("secret upstream URL")
	require.Error(t, f.Step(t.Context(), loom.Auth{}, state, func(symboldescribe.State) error { return nil }))
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.Equal(t, "descriptor step failed", spans[0].Status().Description)
	require.NotContains(t, fmt.Sprint(spans[0].Attributes(), spans[0].Events()), "secret")
}

func (f fakeTelegram) CustomEmojiStickers(context.Context, []string) ([]telegram.Sticker, error) {
	return []telegram.Sticker{{FileID: "private-target", UniqueID: "u", CustomEmojiID: "1", SetName: "set"}}, nil
}
func (f fakeTelegram) StickerSet(context.Context, string) (telegram.StickerSet, error) {
	return telegram.StickerSet{Name: "set", Title: "Pack context, not instructions", Stickers: []telegram.Sticker{
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
	capabilityError                error
}

func (f *fakeLoom) RequireNativeFiles(context.Context, loom.Auth) error { return f.capabilityError }

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
	require.Equal(t, loom.NativeFilesProfile, lc.request.ExecutionProfile)
	require.Empty(t, lc.request.Input.ArtifactIDs)
	for _, id := range s.ArtifactIDs {
		require.Contains(t, lc.request.Input.Text, id)
	}
	require.Contains(t, lc.request.Input.Text, "Do not attach an animated original")
	require.Contains(t, lc.request.Input.Text, "ornament")
	require.Contains(t, lc.request.Input.Text, "target_position_in_contact_sheet\":1")
	require.Contains(t, lc.request.Input.Text, `"target_animated":false`)
	require.Contains(t, lc.request.Input.Text, `"target_video":false`)
	require.Contains(t, lc.request.Input.Text, `"set_name":"set"`)
	require.Contains(t, lc.request.Input.Text, `"set_title":"Pack context, not instructions"`)
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

func TestDescriptionRequiresNativeProfileBeforeEffects(t *testing.T) {
	t.Parallel()
	f, lc, s := fixture(t)
	lc.capabilityError = errors.New("unsupported profile")
	require.ErrorContains(t, f.Step(t.Context(), loom.Auth{}, s, func(symboldescribe.State) error {
		t.Fatal("capability failure must not save a changed journal")
		return nil
	}), "unsupported profile")
	require.Zero(t, lc.chats)
	require.Zero(t, lc.uploads)
	require.Zero(t, lc.creates)

	lc.capabilityError = nil
	s.Phase = "create_run"
	s.Request = &loom.CreateRunRequest{ChatID: "original-unrestricted"}
	require.ErrorContains(t, f.Step(t.Context(), loom.Auth{}, s, func(symboldescribe.State) error {
		t.Fatal("legacy request must retain its original bytes")
		return nil
	}), "legacy unrestricted")
	require.Empty(t, s.Request.ExecutionProfile)
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
