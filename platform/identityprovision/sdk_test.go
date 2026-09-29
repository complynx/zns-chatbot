package identityprovision_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metadata "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/metadata/v2"
	object "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	user "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

type providerClient struct {
	user.UserServiceClient

	create *user.CreateUserRequest
	err    error
}

func (c *providerClient) CreateUser(
	_ context.Context,
	r *user.CreateUserRequest,
	_ ...grpc.CallOption,
) (*user.CreateUserResponse, error) {
	c.create = r
	return &user.CreateUserResponse{Id: r.GetUserId()}, c.err
}

func (c *providerClient) GetUserByID(
	_ context.Context,
	r *user.GetUserByIDRequest,
	_ ...grpc.CallOption,
) (*user.GetUserByIDResponse, error) {
	return &user.GetUserByIDResponse{
		User: &user.User{UserId: r.GetUserId(), Details: &object.Details{ResourceOwner: "org"},
			State: user.UserState_USER_STATE_ACTIVE, Type: &user.User_Human{Human: &user.HumanUser{}}},
	}, c.err
}

func (c *providerClient) ListUserMetadata(
	_ context.Context,
	_ *user.ListUserMetadataRequest,
	_ ...grpc.CallOption,
) (*user.ListUserMetadataResponse, error) {
	return &user.ListUserMetadataResponse{
		Metadata: []*metadata.Metadata{{Key: "zns.provisioning.operation", Value: []byte("operation")}},
	}, c.err
}

func TestSDKCreatesUnverifiedHumanWithoutExternalSubject(t *testing.T) {
	t.Parallel()
	client := &providerClient{}
	sdk := &identityprovision.SDK{Client: client}
	c := identityprovision.Creation{
		Subject:      "reserved-id",
		Organization: "org",
		Operation:    "operation",
		FirstName:    "Synthetic",
		LastName:     "Person",
		Email:        "synthetic@telegram.invalid",
		Language:     "en",
	}
	require.NoError(t, sdk.Create(t.Context(), c))
	human := client.create.GetHuman()
	require.NotNil(t, human.GetEmail().GetReturnCode())
	require.False(t, human.GetEmail().GetIsVerified())
	require.Empty(t, human.GetIdpLinks())
	require.NotEmpty(t, human.GetPassword().GetPassword())
	require.False(t, human.GetPassword().GetChangeRequired())
	require.Equal(t, c.Subject, client.create.GetUserId())
	require.Equal(t, c.Organization, client.create.GetOrganizationId())
	require.Equal(t, []byte(c.Operation), client.create.GetMetadata()[0].GetValue())
	account, err := sdk.Get(t.Context(), c.Subject)
	require.NoError(t, err)
	require.Equal(
		t,
		identityprovision.Account{
			Subject:      c.Subject,
			Organization: c.Organization,
			Operation:    c.Operation,
			Active:       true,
			Human:        true,
		},
		account,
	)
}

func TestSDKErrorsDoNotExposeProviderMessages(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		code codes.Code
		want error
	}{
		{codes.NotFound, identityprovision.ErrNotFound},
		{codes.FailedPrecondition, identityprovision.ErrConflict},
		{codes.Unauthenticated, identityprovision.ErrUnavailable},
	} {
		t.Run(test.code.String(), func(t *testing.T) {
			t.Parallel()
			sdk := &identityprovision.SDK{
				Client: &providerClient{err: status.Error(test.code, "private_provider_details")},
			}
			_, err := sdk.Get(t.Context(), "reserved")
			require.ErrorIs(t, err, test.want)
			require.NotContains(t, err.Error(), "private_provider_details")
		})
	}
}

func TestSDKRejectsUnsafeCredentialDestinations(t *testing.T) {
	t.Parallel()
	source := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic-unused"})
	for _, issuer := range []string{"http://identity.invalid", "https://username:password@identity.invalid", "https://identity.invalid/path", "https://identity.invalid?target=other"} {
		t.Run(issuer, func(t *testing.T) {
			t.Parallel()
			_, err := identityprovision.NewSDK(
				identityprovision.SDKConfig{Issuer: issuer, TokenSource: source, AllowLocalHTTP: true},
			)
			require.ErrorIs(t, err, identityprovision.ErrInvalid)
		})
	}
	_, err := identityprovision.NewSDK(
		identityprovision.SDKConfig{Issuer: "http://localhost:8113", TokenSource: source},
	)
	require.ErrorIs(t, err, identityprovision.ErrInvalid, "plaintext needs explicit stand opt-in")
}
