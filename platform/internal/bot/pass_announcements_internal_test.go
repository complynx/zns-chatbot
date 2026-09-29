package bot

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestRegistrationAnnouncementSourceText(t *testing.T) {
	t.Parallel()
	item := passbooking.RegistrationAnnouncement{Name: "A < B", Role: "leader", Locale: "en"}
	text, err := registrationAnnouncementText(item)
	require.NoError(t, err)
	assert.Equal(t, "A &lt; B applied for a leader pass!", text)
	item.Locale = "ru"
	item.Role = "follower"
	text, err = registrationAnnouncementText(item)
	require.NoError(t, err)
	assert.Equal(t, "A &lt; B подал заявку на пасс партнёрши!", text)
}

func TestRegistrationAnnouncementUncertainSendIsNotRetried(t *testing.T) {
	t.Parallel()
	result := announcementCompletion(1, 0, errors.New("connection lost"))
	assert.Equal(t, "telegram_outcome_unknown", result.Outcome.Reason)
	result = announcementCompletion(1, 0, &telegram.APIError{Code: http.StatusForbidden})
	assert.Equal(t, "telegram_recipient_rejected", result.Outcome.Reason)
	result = announcementCompletion(1, 44, nil)
	assert.Empty(t, result.Outcome.Reason)
	assert.EqualValues(t, 44, result.Outcome.MessageID)
}
