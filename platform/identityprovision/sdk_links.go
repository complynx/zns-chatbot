package identityprovision

import (
	"context"

	object "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	user "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
)

const externalLinkPageLimit = 100

// Links fails closed if the bounded response is incomplete.
func (s *SDK) Links(ctx context.Context, subject string) ([]ExternalIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	response, err := s.Client.ListIDPLinks(
		ctx,
		&user.ListIDPLinksRequest{UserId: subject, Query: &object.ListQuery{Limit: externalLinkPageLimit}},
	)
	if err != nil {
		return nil, providerError(err)
	}
	if response.GetDetails() == nil || response.GetDetails().GetTotalResult() != uint64(len(response.GetResult())) {
		return nil, ErrConflict
	}
	result := make([]ExternalIdentity, 0, len(response.GetResult()))
	for _, link := range response.GetResult() {
		result = append(result, ExternalIdentity{IDP: link.GetIdpId(), Subject: link.GetUserId()})
	}
	return result, nil
}

func (s *SDK) AddLink(ctx context.Context, subject string, link ExternalIdentity) error {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	_, err := s.Client.AddIDPLink(
		ctx,
		&user.AddIDPLinkRequest{
			UserId:  subject,
			IdpLink: &user.IDPLink{IdpId: link.IDP, UserId: link.Subject, UserName: "Telegram"},
		},
	)
	if err != nil {
		return providerError(err)
	}
	return nil
}
