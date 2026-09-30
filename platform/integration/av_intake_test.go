package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type avWorker struct {
	initial, ranges int
	result          mediaproc.Result
	failure         error
}

func (w *avWorker) Preprocess(context.Context, mediaclient.Kind, []byte) (mediaproc.Result, error) {
	w.initial++
	return w.result, w.failure
}

func (w *avWorker) Storyboard(
	_ context.Context,
	_ mediaclient.Kind,
	_ []byte,
	selection mediaclient.Range,
) (mediaproc.Result, error) {
	w.ranges++
	result := w.result
	result.Transcript = mediaproc.Transcript{Status: "not_run"}
	result.Sampling = &mediaproc.Sampling{
		Coverage:  "range_sparse",
		Requested: []mediaproc.Rational{{Numerator: selection.StartMS, Denominator: 1000}},
	}
	return result, w.failure
}

type avModel func(context.Context, agent.Input) (agent.Plan, error)

func (m avModel) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	return m(ctx, input)
}

func avUpload(t *testing.T, f *fixture, kind string) telegram.Update {
	t.Helper()
	response := sandboxMediaRequest(
		t,
		f,
		"/lab/"+kind+"?user=101&filename=synthetic.av",
		[]byte("synthetic media worker fixture"),
	)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	update.ID = 100
	return update
}
func avResult(t *testing.T, video bool) mediaproc.Result {
	t.Helper()
	result := mediaproc.Result{
		Status:     "ready",
		Duration:   mediaproc.Rational{Numerator: 2, Denominator: 1},
		Transcript: mediaproc.Transcript{Status: "ok", Text: "Private recognized speech."},
	}
	if video {
		var body bytes.Buffer
		require.NoError(t, jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
		result.Frames = []mediaproc.Frame{
			{JPEG: body.Bytes(), Width: 2, Height: 2, Timestamp: mediaproc.Rational{Denominator: 1}},
		}
		result.Sampling = &mediaproc.Sampling{
			Coverage:  "uniform_sparse",
			Requested: []mediaproc.Rational{{Denominator: 1}},
		}
	}
	return result
}

func TestAVVoiceDurableRetryAndPrivateCleanup(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, false)}
	worker.result.Transcript.Text = strings.Repeat("s", 64<<10)
	f.b.AV = worker
	update := avUpload(t, f, "voice")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		cancel()
		return agent.Plan{}, context.Canceled
	})
	require.Error(t, f.b.Handle(ctx, update))
	var retained bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT private_result IS NOT NULL FROM bot.av_results").Scan(&retained),
	)
	assert.True(t, retained)
	f.b.Model = f.model
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "Understood."}
	handle(t, f.b, update)
	require.NotNil(t, f.model.input.AV)
	assert.Len(t, f.model.input.AV.Transcript.Text, 64<<10)
	assert.Nil(t, f.model.input.Attachment)
	assert.Equal(t, 1, worker.initial)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT private_result IS NOT NULL FROM bot.av_results").Scan(&retained),
	)
	assert.False(t, retained)
	handle(t, f.b, update)
	assert.Equal(t, 1, worker.initial)
	assert.Equal(t, 1, f.model.calls)
	var leaked bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE content::text LIKE '%ssssssssss%')").
			Scan(&leaked),
	)
	assert.False(t, leaked)
}

func TestAVVideoRefinementBudgetAndInterruption(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, true)}
	f.b.AV = worker
	update := avUpload(t, f, "video")
	f.b.Model = avModel(func(_ context.Context, _ agent.Input) (agent.Plan, error) {
		return agent.Plan{
			View: agent.MediaView,
			MediaAction: &agent.MediaProposal{
				MediaID:    "tg-media-100",
				Intent:     "inspect_video",
				EndMS:      1000,
				FrameCount: 1,
			},
		}, nil
	})
	handle(t, f.b, update)
	assert.Equal(t, 1, worker.initial)
	assert.Equal(t, 2, worker.ranges)
	f.b.Model = f.model
	f.model.plan = agent.Plan{View: "workflow", Text: "Four."}
	handle(t, f.b, message(101, 101, "What is two plus two?"))
	assert.Nil(t, f.model.input.AV)
	assert.Empty(t, f.model.input.Frames)
	assert.Equal(t, 2, worker.ranges)
	require.NotNil(t, f.model.input.MediaContext)
	require.Len(t, f.model.input.MediaContext.Pending, 1)
	assert.True(t, f.model.input.MediaContext.Pending[0].CanInspect)
	assert.EqualValues(t, 2000, f.model.input.MediaContext.Pending[0].DurationMS)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "inspect_video", EndMS: 1000, FrameCount: 1},
	}
	handle(t, f.b, message(102, 101, "Look closer at the start."))
	assert.Equal(t, 4, worker.ranges, "new user input has its own two-round budget")
	assert.Equal(t, 1, worker.initial, "follow-up ranges do not repeat ASR")
}

func TestAVRefinementResultsPermitFinalAnswerAtZeroBudget(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, true)}
	f.b.AV = worker
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		require.NotNil(t, input.AVInspection)
		assert.Equal(t, 2-worker.ranges, input.AVInspection.Remaining)
		require.Len(t, input.AVInspection.Completed, worker.ranges)
		if input.AVInspection.Remaining == 0 {
			require.NotEmpty(t, input.Frames)
			assert.EqualValues(t, 1000, input.AVInspection.Completed[1].StartMS)
			return agent.Plan{View: agent.MediaView, Text: "На полученных кадрах показано 35 BYN."}, nil
		}
		return agent.Plan{
			View: agent.MediaView,
			MediaAction: &agent.MediaProposal{
				MediaID:    "tg-media-100",
				Intent:     "inspect_video",
				StartMS:    int64(worker.ranges) * 1000,
				EndMS:      int64(worker.ranges+1) * 1000,
				FrameCount: 1,
			},
		}, nil
	})
	update := avUpload(t, f, "video_note")
	handle(t, f.b, update)
	assert.Equal(t, 3, calls)
	assert.Equal(t, 2, worker.ranges)
	var saved string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT payload::text FROM interaction.saved_turns WHERE owner='alice' AND update_id=100").
			Scan(&saved),
	)
	assert.Contains(t, saved, "35 BYN")
	handle(t, f.b, update)
	assert.Equal(t, 3, calls)
	assert.Equal(t, 2, worker.ranges)
}

func TestAVConsumedVoiceDoesNotDisplacePendingReceipt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "clarify"},
	}
	handle(t, f.b, photo)
	worker := &avWorker{result: avResult(t, false)}
	f.b.AV = worker
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Your orders."}
	voice := avUpload(t, f, "voice")
	for id := int64(101); id <= 112; id++ {
		voice.ID = id
		handle(t, f.b, voice)
	}
	var active int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT count(*) FROM bot.media_intake WHERE av_kind='voice' AND status<>'done'").
			Scan(&active),
	)
	assert.Zero(t, active)
	calls := f.model.calls
	handle(t, f.b, voice)
	assert.Equal(t, calls, f.model.calls, "delivery retry reuses its durable plan")
	assert.Equal(t, 12, worker.initial)
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Your orders."}
	handle(t, f.b, message(113, 101, "Show orders"))
	require.NotNil(t, f.model.input.MediaContext)
	require.Len(t, f.model.input.MediaContext.Pending, 1)
	assert.Equal(t, "tg-media-100", f.model.input.MediaContext.Pending[0].ID)
}

func TestAVRefinementRetryReportsPersistedBudget(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, true)}
	f.b.AV = worker
	update := avUpload(t, f, "video")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	proposal := agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "inspect_video", EndMS: 1000, FrameCount: 1},
	}
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		if worker.ranges == 1 {
			cancel()
			return agent.Plan{}, context.Canceled
		}
		return proposal, nil
	})
	require.Error(t, f.b.Handle(ctx, update))
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		require.NotNil(t, input.AVInspection)
		assert.Equal(t, 2-worker.ranges, input.AVInspection.Remaining)
		assert.Len(
			t,
			input.AVInspection.Completed,
			worker.ranges-1,
			"prior attempt frames are not falsely reported as available",
		)
		if input.AVInspection.Remaining == 0 {
			return agent.Plan{View: agent.MediaView, Text: "Final answer from available frames."}, nil
		}
		return proposal, nil
	})
	handle(t, f.b, update)
	assert.Equal(t, 2, worker.ranges)
	assert.Equal(t, 1, worker.initial)
}

func TestAVForeignStaleAndActualRangeChecks(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, true)}
	f.b.AV = worker
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "A sampled frame."}
	handle(t, f.b, avUpload(t, f, "video_note"))
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "inspect_video", EndMS: 3000, FrameCount: 1},
	}
	handle(t, f.b, message(101, 101, "Inspect beyond the end."))
	assert.Zero(t, worker.ranges)
	f.model.plan.MediaAction.EndMS = 1000
	handle(t, f.b, message(102, 102, "Inspect Alice's clip."))
	assert.Zero(t, worker.ranges)
	_, err := f.db.Exec(t.Context(), "UPDATE bot.media_intake SET expires_at=now()-interval '1 second'")
	require.NoError(t, err)
	handle(t, f.b, message(103, 101, "Inspect expired video."))
	assert.Zero(t, worker.ranges)
}

func TestAVUnavailableAndDurationRejectionLocalized(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			worker := &avWorker{
				result: mediaproc.Result{
					Status:     "rejected",
					Reason:     "too_long",
					Duration:   mediaproc.Rational{Denominator: 1},
					Transcript: mediaproc.Transcript{Status: "not_run"},
				},
			}
			f.b.AV = worker
			_, err := f.b.API.SetLanguage(t.Context(), "alice", language, false)
			require.NoError(t, err)
			update := avUpload(t, f, "audio")
			handleVisible(t, f.b, update)
			assert.Zero(t, f.model.calls)
			var notice, status string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), "SELECT notice,status FROM bot.media_intake").Scan(&notice, &status),
			)
			assert.Equal(t, "av.too_long", notice)
			assert.Equal(t, "done", status)
			var visible strings.Builder
			for _, card := range chatMessages(t, f, 101) {
				visible.WriteString(card.Text)
			}
			if language == "ru" {
				assert.Contains(t, visible.String(), "длиннее 4 минут")
			} else {
				assert.Contains(t, visible.String(), "longer than 4 minutes")
			}
			worker.failure = errors.New("worker offline")
			update.ID = 101
			handleVisible(t, f.b, update)
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), "SELECT notice FROM bot.media_intake WHERE update_id=101").Scan(&notice),
			)
			assert.Equal(t, "av.failed", notice)
			assert.Zero(t, f.model.calls)
		})
	}
}

func TestAVExpiredPrivateResultAndRevokedRetry(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"expired", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			worker := &avWorker{result: avResult(t, true)}
			f.b.AV = worker
			update := avUpload(t, f, "video")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
				cancel()
				return agent.Plan{}, context.Canceled
			})
			require.Error(t, f.b.Handle(ctx, update))
			var err error
			if mode == "expired" {
				_, err = f.db.Exec(t.Context(), "UPDATE bot.media_intake SET expires_at=now()-interval '1 second'")
			} else {
				_, err = f.db.Exec(t.Context(), "UPDATE core.users SET can_book=false WHERE id='alice'")
			}
			require.NoError(t, err)
			f.b.Model = f.model
			handle(t, f.b, update)
			assert.Zero(t, f.model.calls)
			assert.Equal(t, 1, worker.initial)
			var retained bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), "SELECT private_result IS NOT NULL FROM bot.av_results").Scan(&retained),
			)
			assert.False(t, retained)
		})
	}
}

func TestAVSameIntakeConcurrentPreprocessOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, false)}
	f.b.AV = worker
	update := avUpload(t, f, "voice")
	var plans atomic.Int32
	ready := make(chan struct{})
	f.b.Model = avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
		if plans.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ready:
			return agent.Plan{View: agent.MediaView, Text: "Answered."}, nil
		case <-ctx.Done():
			return agent.Plan{}, ctx.Err()
		}
	})
	var group sync.WaitGroup
	errorsOut := make(chan error, 2)
	for range 2 {
		group.Go(func() { errorsOut <- f.b.Handle(t.Context(), update) })
	}
	group.Wait()
	close(errorsOut)
	for err := range errorsOut {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, worker.initial)
}

func TestAVVoiceSelectsNamedOlderOrder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := intakeOrder(t, f, "first")
	for index := range 7 {
		intakeOrder(t, f, "later-"+strconv.Itoa(index))
	}
	worker := &avWorker{result: avResult(t, false)}
	worker.result.Transcript.Text = "Add shuttle to order " + first.ID
	f.b.AV = worker
	f.model.plan = agent.Plan{
		View:        agent.OrdersView,
		OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: first.ID, Extra: "shuttle"},
	}
	handle(t, f.b, avUpload(t, f, "voice"))
	var included bool
	for _, summary := range f.model.input.Orders {
		if summary.ID == first.ID {
			included = true
		}
	}
	assert.True(t, included, "spoken explicit ID includes an order outside recent five")
	current, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Contains(t, current.Choice.Extras, "shuttle")
	assert.Equal(t, first.Version+1, current.Version)
	assert.Empty(t, f.model.input.Text, "caption remains separate from recognized speech")
}

func TestAVVideoSpeechDoesNotSelectOrder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := intakeOrder(t, f, "first")
	intakeOrder(t, f, "second")
	worker := &avWorker{result: avResult(t, true)}
	worker.result.Transcript.Text = "Add shuttle to order " + first.ID
	f.b.AV = worker
	f.model.plan = agent.Plan{
		View:        agent.OrdersView,
		OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: first.ID, Extra: "shuttle"},
	}
	update := avUpload(t, f, "video")
	update.Message.Caption = "Summarize this recorded conversation."
	handle(t, f.b, update)
	current, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, first.Version, current.Version)
	assert.NotContains(t, current.Choice.Extras, "shuttle")
}

func TestAVVoiceNavigationUsesNormalCards(t *testing.T) {
	t.Parallel()
	for _, view := range []string{agent.OrdersView, agent.ProfilesView} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			worker := &avWorker{result: avResult(t, false)}
			worker.result.Transcript.Text = "Show my " + view
			f.b.AV = worker
			f.model.plan = agent.Plan{View: view, Text: "Here it is."}
			handle(t, f.b, avUpload(t, f, "voice"))
			var planView string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), "SELECT payload->'plan'->>'view' FROM interaction.saved_turns WHERE owner='alice' AND update_id=100").
					Scan(&planView),
			)
			assert.Equal(t, view, planView)
			kind := "orders_reply"
			if view == agent.ProfilesView {
				kind = "profile_answer"
			}
			var rendered bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE update_id=100 AND kind=$1)", kind).
					Scan(&rendered),
			)
			assert.True(t, rendered)
		})
	}
}

func TestAVVoiceResumesDisplayedReceiptSelection(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"ordinal", "exact", "stale", "video_quote"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			testAVReceiptSelection(t, mode)
		})
	}
}

func testAVReceiptSelection(t *testing.T, mode string) {
	f := setup(t)
	intakeOrder(t, f, "first")
	intakeOrder(t, f, "second")
	photo, photoBytes := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View: agent.MediaView,
		MediaAction: &agent.MediaProposal{
			MediaID:  "tg-media-100",
			Intent:   "receipt",
			Amount:   "35",
			Currency: "BYN",
		},
	}
	handle(t, f.b, photo)
	var rendered agent.MediaHint
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT rendered FROM bot.media_intake WHERE id='tg-media-100'").
			Scan(&rendered),
	)
	require.GreaterOrEqual(t, len(rendered.Choices), 2)
	choice := rendered.Choices[0]
	require.Equal(t, "order", choice.Action)
	selected, err := f.b.API.Order(t.Context(), "alice", "sandbox-festival", choice.OrderID)
	require.NoError(t, err)
	if mode == "stale" {
		command := orderCommand("edit", selected)
		command.Choice = orderChoice("shuttle")
		_, err = f.b.API.ExecuteOrder(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	worker := &avWorker{result: avResult(t, mode == "video_quote")}
	worker.result.Transcript.Text = "Use the first displayed order for that receipt."
	if mode == "exact" || mode == "video_quote" {
		worker.result.Transcript.Text = "Use order " + choice.OrderID + " for that receipt."
	}
	f.b.AV = worker
	kind := "voice"
	if mode == "video_quote" {
		kind = "video"
	}
	followup := avUpload(t, f, kind)
	followup.ID = 101
	if mode == "video_quote" {
		followup.Message.Caption = "Summarize this recorded conversation."
	}
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", OrderID: choice.OrderID},
	}
	handle(t, f.b, followup)
	current, err := f.b.API.Order(t.Context(), "alice", selected.EventID, selected.ID)
	require.NoError(t, err)
	if mode == "stale" || mode == "video_quote" {
		assert.Equal(t, "unpaid", current.State)
	} else {
		assert.Equal(t, "proof", current.State)
		proof, proofErr := (orders.Service{DB: f.db}).OrderProof(
			t.Context(),
			"alice",
			selected.EventID,
			selected.ID,
		)
		require.NoError(t, proofErr)
		assert.Equal(
			t,
			photoBytes,
			proof.Body,
			"receipt source stays the earlier photo, never the voice upload",
		)
	}
	var voiceCommand *orders.Command
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT command FROM bot.media_intake WHERE id='tg-media-101'").
			Scan(&voiceCommand),
	)
	assert.Nil(t, voiceCommand)
	var planJSON string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT payload::text FROM interaction.saved_turns WHERE owner='alice' AND update_id=101").
			Scan(&planJSON),
	)
	assert.NotContains(t, planJSON, worker.result.Transcript.Text)
	calls := f.model.calls
	handle(t, f.b, followup)
	assert.Equal(t, calls, f.model.calls, "receipt follow-up delivery reuses the saved plan")
	assert.Equal(t, 1, worker.initial)
	if mode == "stale" {
		var notice string
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), "SELECT notice FROM bot.media_intake WHERE id='tg-media-100'").
				Scan(&notice),
		)
		assert.Equal(t, "media.stale", notice)
	}
}
