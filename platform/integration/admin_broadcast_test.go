package integration_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func TestAdminBroadcastInputOwnershipExpiryReplay(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	input, err := service.BeginInput(t.Context(), "bob", "start", "/send_message_to 101 --forward", 202)
	require.NoError(t, err)
	require.NoError(t, service.RegisterPrompt(t.Context(), "bob", input.ID, 202, 50))
	attachment := adminmessage.Attachment{
		InputID:  input.ID,
		ChatID:   202,
		PromptID: 50,
		Key:      "attach",
	}
	wrong := attachment
	wrong.PromptID = 49
	_, err = service.AttachInput(t.Context(), "bob", wrong)
	requireCode(t, err, "not_found")
	wrong = attachment
	wrong.Key = "unregistered-source"
	_, err = service.AttachInput(t.Context(), "bob", wrong)
	requireCode(t, err, "admin_message_source_unavailable")
	require.NoError(
		t,
		service.RegisterSource(
			t.Context(),
			adminmessage.Source{Actor: "bob", Key: attachment.Key, ChatID: 202, MessageID: 51},
		),
	)
	preview, err := service.AttachInput(t.Context(), "bob", attachment)
	require.NoError(t, err)
	replay, err := service.AttachInput(t.Context(), "bob", attachment)
	require.NoError(t, err)
	assert.Equal(t, preview.ID, replay.ID)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	delivery, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, adminmessage.Content{FromChat: 202, FromMessage: 51}, delivery.Content)
	input, err = service.BeginInput(t.Context(), "bob", "expire", "/send_message_to 101", 202)
	require.NoError(t, err)
	require.NoError(t, service.RegisterPrompt(t.Context(), "bob", input.ID, 202, 60))
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_inputs SET expires_at=clock_timestamp() WHERE id=$1`,
		input.ID,
	)
	require.NoError(t, err)
	_, err = service.AttachInput(
		t.Context(),
		"bob",
		adminmessage.Attachment{
			InputID:  input.ID,
			ChatID:   202,
			PromptID: 60,
			Key:      "expired",
		},
	)
	requireCode(t, err, "admin_message_input_unavailable")
	pending, err := service.PendingInputs(t.Context(), "bob", 202)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestAdminBroadcastLargeAudiencePagedSnapshot(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	var request adminmessage.Request
	request.Content = adminmessage.Content{Text: "stable"}
	for id := 1; id <= 1021; id++ {
		request.Destinations = append(request.Destinations, adminmessage.Destination{Chat: strconv.Itoa(id)})
	}
	preview, err := service.Preview(t.Context(), "bob", "large", request)
	require.NoError(t, err)
	var seen []string
	for offset := int64(0); ; {
		page, pageErr := service.Review(t.Context(), "bob", preview.ID, offset)
		require.NoError(t, pageErr)
		assert.EqualValues(t, 1021, page.Total)
		for _, item := range page.Items {
			seen = append(seen, item.Destination.Chat)
			assert.Equal(t, "stable", item.Content.Text)
		}
		if !page.More {
			break
		}
		offset += int64(len(page.Items))
	}
	require.Len(t, seen, 1021)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	results, err := service.Results(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	assert.Len(t, results, 1021)
	claimed := make(map[string]bool)
	for {
		delivery, found, claimErr := service.Claim(t.Context())
		require.NoError(t, claimErr)
		if !found {
			break
		}
		assert.False(t, claimed[delivery.Destination.Chat])
		claimed[delivery.Destination.Chat] = true
		assert.Equal(t, "stable", delivery.Content.Text)
		require.NoError(t, service.Complete(t.Context(), delivery.ID, delivery.Attempt, 1, "", false))
	}
	assert.Len(t, claimed, 1021)
}

func TestAdminBroadcastInterruptedRenderResumesFrozenProfile(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"Original"}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service := adminmessage.Service{DB: db, InformalName: func(context.Context, map[string]any) (string, error) {
		cancel()
		return "", context.Canceled
	}}
	preview, err := service.PreviewCommand(
		ctx,
		"bob",
		"interrupted",
		`/send_message_to 101 --template --msg '{{.first_name}} {{.user_informal_name}}'`,
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Positive(t, preview.ID)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_broadcast_profiles SET overrides='{"first_name":"Changed"}' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_recipients SET render_available_at=clock_timestamp() WHERE message_id=$1`,
		preview.ID,
	)
	require.NoError(t, err)
	calls := 0
	service.InformalName = func(_ context.Context, fields map[string]any) (string, error) {
		calls++
		assert.Equal(t, "Original", fields["first_name"])
		return "Friend", nil
	}
	resumed, err := service.ResumePreview(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	assert.Equal(t, "draft", resumed.State)
	_, err = service.ResumePreview(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "Original Friend", page.Items[0].Content.Text)
}

func TestAdminBroadcastCommandReplayPreservesAudience(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	command := `/send_message_to $dance:admins --msg "stable"`
	first, err := service.PreviewCommand(t.Context(), "bob", "audience", command)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE event_id='dance'`)
	require.NoError(t, err)
	replay, err := service.PreviewCommand(t.Context(), "bob", "audience", command)
	require.NoError(t, err)
	assert.Equal(t, first, replay)
}

func TestAdminBroadcastTemplateSnapshotAndNameCache(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"<Alice>","known_names":["Alice"]}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	calls := 0
	service := adminmessage.Service{
		DB: db,
		InformalName: func(ctx context.Context, names map[string]any) (string, error) {
			calls++
			assert.Equal(t, "<Alice>", names["first_name"])
			// This would time out if rendering retained the draft row lock.
			callbackCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_, writeErr := db.Exec(callbackCtx, `UPDATE core.admin_messages SET state=state WHERE actor='bob'`)
			return "Alice", writeErr
		},
	}
	command := `/send_message_to 101 --template --html '{{.first_name}} / {{.user_informal_name}} / {{template "user_link" .}}'`
	preview, err := service.PreviewCommand(t.Context(), "bob", "template", command)
	require.NoError(t, err)
	assert.Equal(t, "draft", preview.State)
	assert.Equal(t, 1, calls)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Empty(t, page.Items[0].Failure)
	assert.Contains(t, page.Items[0].Content.Text, "&lt;Alice&gt; / Alice")
	original := page.Items[0].Content
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_broadcast_profiles SET overrides='{"first_name":"Changed","informal_name":"Changed"}' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	replay, err := service.PreviewCommand(t.Context(), "bob", "template", command)
	require.NoError(t, err)
	assert.Equal(t, preview.ID, replay.ID)
	assert.Equal(t, 1, calls)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	delivery, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, original, delivery.Content)
}

func TestAdminBroadcastTemplateErrorsCannotEnqueue(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"Alice","informal_name":null}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"missing",
		`/send_message_to 101 --template --msg '{{.missing}}'`,
	)
	require.NoError(t, err)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.NotEmpty(t, page.Items[0].Failure)
	requireCode(t, service.Enqueue(t.Context(), "bob", preview.ID), "admin_message_render_failed")
}

func TestAdminBroadcastSnapshotPreservesNumericLink(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	_, err := db.Exec(t.Context(), `UPDATE core.users SET telegram_id=9007199254740993 WHERE id='alice'`)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"<Alice>","informal_name":null}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"numeric-link",
		`/send_message_to 9007199254740993 --template --html '{{if gt .user_id 9007199254740992}}{{template "user_link" .}}{{end}}'`,
	)
	require.NoError(t, err)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, `<a href="tg://user?id=9007199254740993">&lt;Alice&gt;</a>`, page.Items[0].Content.Text)
}

func TestAdminBroadcastInputExpiryRecovery(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	input, err := service.BeginInput(t.Context(), "bob", "expiry-notice", "/send_message_to 101", 202)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_inputs SET expires_at=clock_timestamp() WHERE id=$1`,
		input.ID,
	)
	require.NoError(t, err)
	claim, found, err := service.ClaimInputExpiry(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	_, again, err := service.ClaimInputExpiry(t.Context())
	require.NoError(t, err)
	assert.False(t, again)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_inputs SET notice_available_at=clock_timestamp() WHERE id=$1`,
		input.ID,
	)
	require.NoError(t, err)
	recovered, found, err := service.ClaimInputExpiry(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Greater(t, recovered.Attempt, claim.Attempt)
	requireCode(t, service.CompleteInputExpiry(t.Context(), claim.ID, claim.Attempt), "admin_message_stale_attempt")
	require.NoError(t, service.CompleteInputExpiry(t.Context(), recovered.ID, recovered.Attempt))
	_, found, err = service.ClaimInputExpiry(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAdminBroadcastAudienceCompleteAndChunked(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) SELECT id,jsonb_build_object('first_name',id,'user_id',telegram_id) FROM core.users ON CONFLICT(owner) DO NOTHING`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.users(id,telegram_id,name,can_book) SELECT 'audience-'||i,80000+i,'Audience',true FROM generate_series(1,25)i`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) SELECT id,jsonb_build_object('first_name',id,'known_names',repeat('😀"',5000)) FROM core.users WHERE id LIKE 'audience-%'`,
	)
	require.NoError(t, err)
	page, err := service.Audience(t.Context(), "bob", "")
	require.NoError(t, err)
	require.True(t, page.More)
	require.Len(t, page.Items, 20)
	_, err = service.Audience(t.Context(), "alice", page.NextCursor)
	requireCode(t, err, "forbidden")
	next, err := service.Audience(t.Context(), "bob", page.NextCursor)
	require.NoError(t, err)
	assert.False(t, next.More)
	seen := map[string]bool{}
	for _, item := range append(page.Items, next.Items...) {
		assert.False(t, seen[item.UserID])
		seen[item.UserID] = true
	}
	assert.Len(t, seen, 28)
	chunk, err := service.AudienceProfile(t.Context(), "bob", "80001", "")
	require.NoError(t, err)
	require.True(t, chunk.More)
	var all strings.Builder
	all.WriteString(chunk.JSON)
	cursor := chunk.NextCursor
	for cursor != "" {
		part, partErr := service.AudienceProfile(t.Context(), "bob", "80001", cursor)
		require.NoError(t, partErr)
		all.WriteString(part.JSON)
		cursor = part.NextCursor
	}
	var profile map[string]any
	require.NoError(t, json.Unmarshal([]byte(all.String()), &profile))
	assert.Equal(t, strings.Repeat("😀\"", 5000), profile["known_names"])
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_broadcast_profiles SET overrides='{"first_name":"changed"}' WHERE owner='audience-1'`,
	)
	require.NoError(t, err)
	_, err = service.AudienceProfile(t.Context(), "bob", "80001", chunk.NextCursor)
	requireCode(t, err, "read_stale")
}

func TestAdminBroadcastTrustedDeploymentIdentity(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"bot_id":666,"informal_name":null}'),('bob','{"informal_name":null}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	service := adminmessage.Service{DB: db, BotID: 77}
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) SELECT id,'{}' FROM core.users ON CONFLICT(owner) DO NOTHING`,
	)
	require.NoError(t, err)
	for _, id := range []string{"101", "202"} {
		chunk, readErr := service.AudienceProfile(t.Context(), "bob", id, "")
		require.NoError(t, readErr)
		assert.Contains(t, chunk.JSON, `"bot_id":77`)
		assert.NotContains(t, chunk.JSON, "666")
	}
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"trusted-bot",
		`/send_message_to '[101,202]' --template --msg '{{.bot_id}}'`,
	)
	require.NoError(t, err)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	for _, item := range page.Items {
		assert.Equal(t, "77", item.Content.Text)
	}
	selected, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"namespace-filter",
		`/send_message_to '{"$and":[{"bot_id":77},{"user_id":{"$in":[101,202]}}]}' --msg scoped`,
	)
	require.NoError(t, err)
	assert.Len(t, selected.Request.Destinations, 2)
	service.BotID = 0
	chunk, err := service.AudienceProfile(t.Context(), "bob", "101", "")
	require.NoError(t, err)
	assert.NotContains(t, chunk.JSON, "bot_id")
	var source int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (fields->>'bot_id')::int FROM core.admin_broadcast_profiles WHERE owner='alice'`).
			Scan(&source),
	)
	assert.Equal(t, 666, source, "read-time identity overlay must not mutate imported evidence")
}
