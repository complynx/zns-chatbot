package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/render"
)

type paymentDescriptionPreparation struct {
	Description   string                `json:"description"`
	Arguments     payments.ReviewChange `json:"arguments"`
	Preconditions struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
}

func paymentDescriptionLocale(t *testing.T, f paymentFixture, preference, telegram, fallback string) {
	t.Helper()
	_, err := f.H.store.Pool().Exec(t.Context(), "INSERT INTO shuttle_user_profiles(user_id,version,bot_language)\nVALUES($" +
"1,0,$2) ON CONFLICT(user_id) DO UPDATE SET bot_language=$2,version=shutt" +
"le_user_profiles.version+1", f.AmbassadorID, preference)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_users SET profile=jsonb_set(profile,'{language_code}',to_" +
"jsonb($2::text),true) WHERE id=$1", f.AmbassadorID, telegram)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_settings SET document=jsonb_set(document,'{default_langua" +
"ge}',to_jsonb($1::text)) WHERE id=true", fallback)
	require.NoError(t, err)
}

func TestPaymentReviewPreparationUsesReviewerLanguageAndKeepsOriginalReplay(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, preference, telegram, fallback, title string }{
		{"explicit ru", "ru", "en", "en", "Подтвердить оплату?"},
		{"explicit en", "en", "ru", "ru", "Confirm payment?"},
		{"ukrainian", "auto", "uk-UA", "en", "Подтвердить оплату?"},
		{"belarusian", "auto", "be_BY", "en", "Подтвердить оплату?"},
		{"russian", "auto", "ru-RU", "en", "Подтвердить оплату?"},
		{"other", "auto", "nl-NL", "ru", "Confirm payment?"},
		{"default", "auto", "", "ru", "Подтвердить оплату?"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, review := paymentWorkspaceAPIFixture(t)
			paymentDescriptionLocale(t, f, test.preference, test.telegram, test.fallback)
			code, raw := paymentRequest(t, f, f.Ambassador, "review/prepare", "readable-review", review)
			require.Equal(t, http.StatusOK, code, string(raw))
			var prepared paymentDescriptionPreparation
			require.NoError(t, json.Unmarshal(raw, &prepared))
			require.True(t, strings.HasPrefix(prepared.Description, test.title))
			require.Equal(t, review, prepared.Arguments)
			require.NotEqual(t, uuid.Nil, prepared.Preconditions.ProposalID)
			for _, value := range []string{"30.87 BYN", "40.00 BYN", "123.45 BYN", "92.58 BYN", "Europe/Minsk", f.Event.Document.Name} {
				require.Contains(t, prepared.Description, value)
			}
			paymentDescriptionLocale(t, f, "en", "en", "en")
			code, replay := paymentRequest(t, f, f.Ambassador, "review/prepare", "readable-review", review)
			require.Equal(t, http.StatusOK, code, string(replay))
			var originalBody, replayBody map[string]any
			require.NoError(t, json.Unmarshal(raw, &originalBody))
			require.NoError(t, json.Unmarshal(replay, &replayBody))
			// Each helper call starts a server on a new port; only its schema URL changes.
			delete(originalBody, "$schema")
			delete(replayBody, "$schema")
			require.Equal(t, originalBody, replayBody, "Presentation changes must not rewrite an existing proposal")
			var reviews int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), `SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1`, review.EvidenceID).
					Scan(&reviews),
			)
			require.Zero(t, reviews)
		})
	}
}

func TestPaymentReviewRejectionDescriptionKeepsMetadataLiteralAndCreditsZero(t *testing.T) {
	t.Parallel()
	f, review := paymentWorkspaceAPIFixture(t)
	paymentDescriptionLocale(t, f, "ru", "en", "en")
	review.Decision, review.AmountMinor = payments.Rejected, 0
	review.Reason = "**untrusted** ```\nnot a command [link](https://example.invalid)"
	payer := "Payer **literal** ``` @example"
	_, err := f.H.store.Pool().Exec(t.Context(), "INSERT INTO shuttle_user_profiles(user_id,version,full_name)\nVALUES($1,0" +
",$2) ON CONFLICT(user_id) DO UPDATE SET full_name=$2,version=shuttle_use" +
"r_profiles.version+1", f.OwnerID, payer)
	require.NoError(t, err)
	code, raw := paymentRequest(t, f, f.Ambassador, "review/prepare", "readable-rejection", review)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared paymentDescriptionPreparation
	require.NoError(t, json.Unmarshal(raw, &prepared))
	var shown strings.Builder
	for _, message := range render.Render(prepared.Description, nil) {
		require.NoError(t, render.Validate(message))
		shown.WriteString(message.Text)
		for _, entity := range message.Entities {
			require.Equal(t, "pre", entity.Type, "Proof metadata must not create links, emphasis or mentions")
		}
	}
	for _, value := range []string{"Отклонить подтверждение оплаты?", "Зачислить: 0.00 BYN", "Остаток после этого решения: 123.45 BYN", strconv.Quote(payer), strconv.Quote(review.Reason)} {
		require.Contains(t, shown.String(), value)
	}
	require.Equal(t, review, prepared.Arguments)
	var reviews int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), `SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1`, review.EvidenceID).
			Scan(&reviews),
	)
	require.Zero(t, reviews)
}
