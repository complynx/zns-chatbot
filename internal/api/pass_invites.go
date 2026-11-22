package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type issuePassInviteRequest struct {
	AuthInput

	ID   uuid.UUID `path:"id"`
	Body struct {
		OperationID   string    `json:"operation_id" minLength:"1" maxLength:"128"`
		ReplacePairID uuid.UUID `json:"replace_pair_id,omitempty"`
	}
}
type claimPassInviteRequest struct {
	AuthInput

	ID   uuid.UUID `path:"id"`
	Body struct {
		Code string `json:"code" minLength:"1" maxLength:"64"`
	}
}
type adminDissolveRequest struct {
	AuthInput

	ID     uuid.UUID `path:"id"`
	PairID uuid.UUID `path:"pair"`
	Body   struct {
		OperationID     string `json:"operation_id" minLength:"1" maxLength:"128"`
		ExpectedVersion int64  `json:"expected_version" minimum:"1"`
	}
}
type passInviteResponse struct{ Body db.PassInvite }
type passDissolveResponse struct{ Body registrations.Pair }

func (s *Server) registerPassInvites(api huma.API) {
	const path = "/internal/events/{id}/pass-invites"
	huma.Register(
		api,
		huma.Operation{
			Method:       http.MethodPost,
			Path:         path,
			OperationID:  "pass-invite-issue",
			MaxBodyBytes: internalBodyLimit,
		},
		s.issuePassInvite,
	)
	huma.Register(
		api,
		huma.Operation{
			Method:       http.MethodPost,
			Path:         path + "/claim",
			OperationID:  "pass-invite-claim",
			MaxBodyBytes: internalBodyLimit,
		},
		s.claimPassInvite,
	)
	huma.Register(
		api,
		huma.Operation{
			Method:       http.MethodPost,
			Path:         path + "/revoke",
			OperationID:  "pass-invite-revoke",
			MaxBodyBytes: internalBodyLimit,
		},
		s.revokePassInvite,
	)
	huma.Register(
		api,
		huma.Operation{
			Method:       http.MethodPost,
			Path:         "/internal/admin/events/{id}/pairs/{pair}/dissolve",
			OperationID:  "pass-pair-admin-dissolve",
			MaxBodyBytes: internalBodyLimit,
		},
		s.adminDissolvePassPair,
	)
}
func (s *Server) issuePassInvite(ctx context.Context, in *issuePassInviteRequest) (*passInviteResponse, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.IssuePassInvite(ctx, scope, in.ID, in.Body.OperationID, in.Body.ReplacePairID)
	if err != nil {
		return nil, pairParticipantError(err)
	}
	return &passInviteResponse{result}, nil
}
func (s *Server) claimPassInvite(ctx context.Context, in *claimPassInviteRequest) (*passInviteResponse, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ClaimPassInvite(ctx, scope, in.ID, in.Body.Code)
	if err != nil {
		return nil, pairParticipantError(err)
	}
	return &passInviteResponse{result}, nil
}
func (s *Server) revokePassInvite(ctx context.Context, in *claimPassInviteRequest) (*struct{}, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if err = s.store.RevokePassInvite(ctx, scope, in.ID, in.Body.Code); err != nil {
		return nil, pairParticipantError(err)
	}
	return &struct{}{}, nil
}
func (s *Server) adminDissolvePassPair(ctx context.Context, in *adminDissolveRequest) (*passDissolveResponse, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	pair, err := s.store.DissolvePairAsAdmin(
		ctx,
		scope,
		in.Body.OperationID,
		db.PairDissolution{EventID: in.ID, PairID: in.PairID, ExpectedVersion: in.Body.ExpectedVersion},
	)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	return &passDissolveResponse{pair}, nil
}
