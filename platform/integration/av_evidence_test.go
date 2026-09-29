package integration_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

type comparisonWorker struct {
	avWorker

	images [][]byte
}

func (w *comparisonWorker) Storyboard(
	_ context.Context,
	_ mediaclient.Kind,
	_ []byte,
	selection mediaclient.Range,
) (mediaproc.Result, error) {
	result := w.result
	result.Transcript = mediaproc.Transcript{Status: "not_run"}
	result.Frames = []mediaproc.Frame{{JPEG: w.images[w.ranges], Width: 2, Height: 2,
		Timestamp: mediaproc.Rational{Numerator: selection.StartMS, Denominator: 1000}}}
	w.ranges++
	return result, nil
}

func coloredFrame(t *testing.T, shade color.RGBA) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			picture.SetRGBA(x, y, shade)
		}
	}
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, picture, nil))
	return encoded.Bytes()
}

func TestAVSpokenComparisonRetainsRequestAndBothRanges(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &comparisonWorker{images: [][]byte{
		coloredFrame(t, color.RGBA{R: 255, A: 255}), coloredFrame(t, color.RGBA{B: 255, A: 255}),
	}}
	worker.result = avResult(t, true)
	f.b.AV = worker
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "Video received."}
	handle(t, f.b, avUpload(t, f, "video"))
	worker.result = avResult(t, false)
	const question = "Compare the sign at the beginning and end of my earlier video."
	worker.result.Transcript.Text = question
	voice := avUpload(t, f, "voice")
	voice.ID = 101
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		require.NotNil(t, input.AV)
		assert.Equal(t, "voice", input.AV.Kind, "inspecting a previous video must not replace the current request")
		assert.Equal(t, question, input.AV.Transcript.Text)
		if worker.ranges == 2 {
			require.Len(t, input.Frames, 2, "a comparison requires both inspected intervals")
			assert.Equal(t, worker.images[0], input.Frames[0].Body)
			assert.Equal(t, worker.images[1], input.Frames[1].Body)
			assert.EqualValues(t, 0, input.Frames[0].TimestampMS)
			assert.EqualValues(t, 1000, input.Frames[1].TimestampMS)
			require.Len(t, input.AVInspection.Completed, 2)
			for _, inspected := range input.AVInspection.Completed {
				assert.Equal(t, 1, inspected.FrameCount)
			}
			return agent.Plan{View: "workflow", Text: "The first sign is red; the second is blue."}, nil
		}
		return agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
			MediaID: "tg-media-100", Intent: "inspect_video", StartMS: int64(worker.ranges) * 1000,
			EndMS: int64(worker.ranges+1) * 1000, FrameCount: 1,
		}}, nil
	})
	handle(t, f.b, voice)
	assert.Equal(t, 2, worker.ranges)
}
