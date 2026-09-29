package identityprovision_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	object "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	user "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"google.golang.org/grpc"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

type externalSDKClient struct {
	user.UserServiceClient

	added *user.AddIDPLinkRequest
	total uint64
}

func (c *externalSDKClient) AddIDPLink(
	_ context.Context,
	r *user.AddIDPLinkRequest,
	_ ...grpc.CallOption,
) (*user.AddIDPLinkResponse, error) {
	c.added = r
	return &user.AddIDPLinkResponse{}, nil
}

func (c *externalSDKClient) ListIDPLinks(
	_ context.Context,
	_ *user.ListIDPLinksRequest,
	_ ...grpc.CallOption,
) (*user.ListIDPLinksResponse, error) {
	return &user.ListIDPLinksResponse{
		Details: &object.ListDetails{TotalResult: c.total},
		Result:  []*user.IDPLink{{IdpId: "relay", UserId: "telegram:77:opaque"}},
	}, nil
}

func TestSDKExternalLinkExactRequestAndCompleteReadback(t *testing.T) {
	t.Parallel()
	client := &externalSDKClient{total: 1}
	sdk := &identityprovision.SDK{Client: client}
	link := identityprovision.ExternalIdentity{IDP: "relay", Subject: "telegram:77:opaque"}
	require.NoError(t, sdk.AddLink(t.Context(), "reserved", link))
	require.Equal(t, "reserved", client.added.GetUserId())
	require.Equal(t, link.Subject, client.added.GetIdpLink().GetUserId())
	require.Equal(t, link.IDP, client.added.GetIdpLink().GetIdpId())
	links, err := sdk.Links(t.Context(), "reserved")
	require.NoError(t, err)
	require.Equal(t, []identityprovision.ExternalIdentity{link}, links)
	client.total = 101
	_, err = sdk.Links(t.Context(), "reserved")
	require.ErrorIs(t, err, identityprovision.ErrConflict)
}
