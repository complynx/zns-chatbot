package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func TestAvatarCropOpenSurvivesConcurrentTransportFinishAndReset(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	f.h.server = httptest.NewServer(f.h.endpoint.PublicHandler())
	deployment := avatarDeployment(f)
	n, err := f.h.store.AvatarDelivery(t.Context(), deployment, receipt.DeliveryID)
	require.NoError(t, err)
	_, err = f.h.store.Pool().Exec(t.Context(),
		`UPDATE shuttle_avatar_deliveries SET receipt='{"old_transport":"must disappear"}' WHERE id=$1`, n.ID)
	require.NoError(t, err)
	reserved, _, err := f.h.store.ReserveAvatarDelivery(t.Context(), f.scope, n.ID, n.Version, uuid.New())
	require.NoError(t, err)
	var wg sync.WaitGroup
	var finishErr error
	wg.Go(func() {
		finishErr = f.h.store.FinishAvatarDelivery(t.Context(), reserved, "delivery_unknown",
			json.RawMessage(`{"new_transport":"only current receipt"}`), 0)
	})
	op := uuid.New()
	code, raw, _ := appCall(t, f.h, http.MethodPost, "/app/avatars/drafts/"+receipt.DraftID.String()+"/open",
		`{"operation_id":"`+op.String()+`"}`, f.session.Token, appTestOrigin)
	wg.Wait()
	require.NoError(t, finishErr)
	require.Equal(t, http.StatusOK, code, string(raw))
	n, err = f.h.store.AvatarDelivery(t.Context(), deployment, n.ID)
	require.NoError(t, err)
	var transport struct {
		UI json.RawMessage `json:"ui"`
	}
	require.NoError(t, json.Unmarshal(n.Receipt, &transport))
	require.Contains(t, string(transport.UI), "crop_opened_at")
	require.NotContains(t, string(n.Receipt), "old_transport")
	require.Contains(t, string(n.Receipt), "new_transport")
	// Exercise the maintained recovery query: old transport data resets, UI facts survive.
	_, err = sqlc.New(f.h.store.Pool()).
		ApplyAvatarDeliveryResolution(t.Context(), sqlc.ApplyAvatarDeliveryResolutionParams{
			ID: n.ID, Outcome: "pending", ExpectedVersion: n.Version,
		})
	require.NoError(t, err)
	n, err = f.h.store.AvatarDelivery(t.Context(), deployment, n.ID)
	require.NoError(t, err)
	require.NotContains(t, string(n.Receipt), "new_transport")
	var reset struct {
		UI json.RawMessage `json:"ui"`
	}
	require.NoError(t, json.Unmarshal(n.Receipt, &reset))
	require.JSONEq(t, string(transport.UI), string(reset.UI))
	current, _, err := f.h.store.GetFinalizedAvatar(t.Context(), f.scope, receipt.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, receipt, current)
	var opened db.AvatarCropOpen
	require.NoError(t, json.Unmarshal(raw, &opened))
	require.Equal(t, receipt.DraftID, opened.DraftID)
}

func TestAvatarCropOpenAllowsOwnedAgentArtifactAndRejectsForeignSession(t *testing.T) {
	t.Parallel()
	f, draft := avatarFinalizationAPISetup(t, nil)
	code, raw := avatarFinalizeAgent(t, f, uuid.New(), draft.ID, draft.Version)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := agentAvatarReceipt(t, raw)
	var native bool
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(),
		"SELECT native FROM shuttle_avatars WHERE id=$1", receipt.ArtifactID).Scan(&native))
	require.False(t, native)
	path := "/app/avatars/drafts/" + draft.ID.String() + "/open"
	body := `{"operation_id":"` + uuid.NewString() + `"}`
	code, raw, _ = appCall(t, f.h, http.MethodPost, path, body, f.session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	_, _ = registrationUser(t, f.h, 89221, false)
	foreign := detailsAppSession(t, f.h, 89221, 233)
	code, raw, _ = appCall(t, f.h, http.MethodPost, path, body, foreign.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	later, _, err := f.h.store.Intake(t.Context(), db.IntakeRequest{BotID: 123, UpdateID: 29321,
		TelegramUserID: 456, ChatID: 456, Payload: json.RawMessage(`{"kind":"message"}`)}, nil)
	require.NoError(t, err)
	actions, err := f.h.store.RecentNativeAvatarDraftActions(t.Context(), f.scope, later.ID)
	require.NoError(t, err)
	require.NotEmpty(t, actions)
	require.Equal(t, "crop_open", actions[0].Kind)
	require.Equal(t, &receipt.ArtifactID, actions[0].ArtifactID)
}
