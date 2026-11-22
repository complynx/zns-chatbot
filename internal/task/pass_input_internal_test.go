package task

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPassPrefillEncryptsBeforeIntakeAndSurvivesStartHandoff(t *testing.T) {
	t.Parallel()
	const text = `/start /passes summer-zouk --role follower --legal-name "Casey Example" ` +
		`--passport-number PP012345 --passport-country NL`
	protector, err := registrationdetails.NewProtector(make([]byte, 32))
	require.NoError(t, err)
	service := Service{options: Options{BotID: 42, RegistrationDetailsProtector: protector}}
	update := telegram.Update{ID: 1, Message: &telegram.Message{ID: 3, Text: text,
		Chat: telegram.Chat{ID: 99, Type: "private"}, From: &telegram.User{ID: 99}}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	input, err := normalize(update, raw)
	require.NoError(t, err)
	require.NoError(t, service.preparePassInput(&input))
	require.Equal(t, "/start /passes", input.Text)
	require.Equal(t, "summer-zouk", input.PassInput.Event)
	require.Equal(t, "follower", input.PassInput.Role)
	require.False(t, input.PassInput.Invalid)
	payload, err := json.Marshal(input)
	require.NoError(t, err)
	for _, private := range []string{"Casey Example", "PP012345", "passport-country"} {
		require.NotContains(t, string(payload), private)
	}
	applyStartPayload(&input, startPayload(input.Text))
	require.Equal(t, kindStructured, input.Kind)
	require.Equal(t, passesCommand, input.StructuredCommand)
	plaintext, err := protector.Open(service.passInputOwner(input.User.ID), input.PassInput.Details)
	require.NoError(t, err)
	require.Contains(t, plaintext, "Casey Example")
	require.Contains(t, plaintext, "PP012345")
	_, err = protector.Open(service.passInputOwner(input.User.ID+1), input.PassInput.Details)
	require.ErrorIs(t, err, registrationdetails.ErrProtection)
}

func TestPassCallbackStaysStructuredThroughIntakeSanitization(t *testing.T) {
	t.Parallel()
	const data = "ps:01234567-89ab-cdef-0123-456789abcdef:0"
	update := telegram.Update{ID: 1, CallbackQuery: &telegram.CallbackQuery{
		From: telegram.User{ID: 99}, Data: data,
		Message: &telegram.Message{ID: 7, Chat: telegram.Chat{ID: 99, Type: "private"},
			ReplyToMessage: &telegram.Message{ID: 5, Caption: "/passes summer --passport-number PP012345"}},
	}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	input, err := normalize(update, raw)
	require.NoError(t, err)
	var service Service
	require.NoError(t, service.preparePassInput(&input))
	require.Equal(t, passesCommand, input.StructuredCommand)
	require.EqualValues(t, 7, input.CallbackMessageID)
	require.Equal(t, "0", input.PassCallback)
	require.Nil(t, input.PassInput)
	require.NotContains(t, string(input.RawUpdate), "PP012345")
}

func TestPassReplyRedactsQuotedPrefill(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(
		`{"message":{"text":"Ready","reply_to_message":{"message_id":8,` +
			`"text":"/passes summer --passport-number PP012345"},"quote":{"text":"PP012345"}}}`,
	)
	redacted, err := redactPassUpdate(raw)
	require.NoError(t, err)
	require.NotContains(t, string(redacted), "PP012345")
	require.Contains(t, string(redacted), `"message_id":8`)
	require.Contains(t, string(redacted), `"text":"Ready"`)
}

func TestPassPrefillOutsideNativeRoute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, publicText, excerpt string }{
		{"forward", `"text":"/passes summer --legal-name Casey --passport-number PP012345",` +
			`"forward_origin":{"type":"hidden_user","sender_user_name":"Original"}`,
			"/passes", `"text":"/passes"`},
		{"custom emoji", `"text":"/start /passes summer --legal-name Casey --passport-number PP012345 🙂",` +
			`"entities":[{"type":"custom_emoji","offset":68,"length":2,"custom_emoji_id":"123456"}]`,
			"/start /passes", `"text":"/start /passes"`},
		{"caption", `"caption":"/passes summer --passport-number PP012345",` +
			`"document":{"file_id":"file","file_unique_id":"id","file_name":"PP012345"}`,
			"/passes", `"caption":"/passes"`},
		{"unsupported", `"caption":"/passes summer --passport-number PP012345","paid_media":{}`,
			"", `"caption":"/passes"`},
		{"addressed forward", `"text":"/passes@ExampleBot summer --passport-number PP012345",` +
			`"forward_origin":{"type":"hidden_user","sender_user_name":"Original"}`,
			"/passes", `"text":"/passes"`},
		{"addressed start", `"text":"/start@ExampleBot /passes summer --passport-number PP012345"`,
			"/start /passes", `"text":"/start /passes"`},
		{"addressed payload", `"text":"/start /passes@ExampleBot summer --passport-number PP012345"`,
			"/start /passes", `"text":"/start /passes"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := json.RawMessage(`{"update_id":1,"message":{"message_id":3,"date":123,"from":{"id":99},` +
				`"chat":{"id":99,"type":"private"},"link_preview_options":{"url":"https://example.org/PP012345"},` +
				tc.body + `}}`)
			var update telegram.Update
			require.NoError(t, json.Unmarshal(raw, &update))
			input, err := normalize(update, raw)
			require.NoError(t, err)
			var service Service
			require.NoError(t, service.preparePassInput(&input))
			require.Equal(t, kindUnsupported, input.Kind)
			require.Equal(t, tc.publicText, input.Text)
			require.JSONEq(t, `{"update_id":1,"message":{"message_id":3,`+tc.excerpt+`}}`, string(input.RawUpdate))
			require.Empty(t, input.StructuredCommand)
			require.Nil(t, input.PassInput)
			require.Nil(t, service.passTicketAtIntake(input))
			require.Nil(t, service.passInviteAtIntake(input))
			require.Empty(t, input.Symbols)
			require.Empty(t, input.Attachments)
			payload, err := json.Marshal(input)
			require.NoError(t, err)
			for _, value := range []string{"PP012345", "Casey", "passport-number", "custom_emoji"} {
				require.NotContains(t, string(payload), value)
			}
		})
	}
}
