package identityprovision_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	user "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

// Explicit opt-in; only the fixed loopback synthetic stand is addressed.
func TestSDKLocalZitadelProvisioning(t *testing.T) {
	t.Parallel()
	statePath := os.Getenv("ZITADEL_PROVISIONING_PROOF_STATE")
	if statePath == "" {
		t.Skip("set ZITADEL_PROVISIONING_PROOF_STATE for the local synthetic stand")
	}
	var state struct {
		Org struct {
			Org struct {
				ID string `json:"id"`
			} `json:"org"`
		} `json:"org"`
	}
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &state))
	pat, err := os.ReadFile(filepath.Join(filepath.Dir(statePath), "admin.pat"))
	require.NoError(t, err)
	sdk, err := identityprovision.NewSDK(
		identityprovision.SDKConfig{Issuer: "http://localhost:8113", AllowLocalHTTP: true,
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: strings.TrimSpace(string(pat))})},
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sdk.Close()) })
	id := "zns-sdk-proof-" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
		defer cancel()
		_, deleteErr := sdk.Client.DeleteUser(ctx, &user.DeleteUserRequest{UserId: id})
		if deleteErr != nil && status.Code(deleteErr) != codes.NotFound {
			t.Error("own synthetic provider user cleanup failed")
		}
	})
	creation := identityprovision.Creation{Subject: id, Organization: state.Org.Org.ID, Operation: uuid.NewString(),
		FirstName: "SDK", LastName: "Synthetic", Email: id + "@telegram.invalid", Language: "en"}
	require.NoError(t, sdk.Create(t.Context(), creation))
	account, err := sdk.Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(
		t,
		identityprovision.Account{
			Subject:      id,
			Organization: creation.Organization,
			Operation:    creation.Operation,
			Human:        true,
			Active:       true,
		},
		account,
	)
	details, err := sdk.Client.GetUserByID(t.Context(), &user.GetUserByIDRequest{UserId: id})
	require.NoError(t, err)
	require.False(t, details.GetUser().GetHuman().GetEmail().GetIsVerified())
	links, err := sdk.Client.ListIDPLinks(t.Context(), &user.ListIDPLinksRequest{UserId: id})
	require.NoError(t, err)
	require.Empty(t, links.GetResult())
	require.ErrorIs(t, sdk.Create(t.Context(), creation), identityprovision.ErrConflict)
	again, err := sdk.Get(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, account, again)
}
