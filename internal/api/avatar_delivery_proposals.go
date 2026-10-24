package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const avatarProposalOperationField = "operation_id"

const avatarProposalPath = "/internal/agent/administration/avatar_delivery_proposal"
const avatarPendingPath = "/internal/agent/administration/avatar_delivery_pending"

type avatarPendingArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int       `json:"limit,omitempty" minimum:"0" maximum:"20"`
}

func (s *Server) registerAvatarDeliveryProposals(api huma.API) {
	registerAgentPost(
		api,
		"agent-avatar-delivery-proposal",
		avatarProposalPath,
		internalBodyLimit,
		s.readAvatarDeliveryProposal,
	)
	registerAgentPost(
		api,
		"agent-avatar-delivery-pending",
		avatarPendingPath,
		internalBodyLimit,
		s.readPendingAvatarDeliveries,
	)
}

func (s *Server) readAvatarDeliveryProposal(
	ctx context.Context,
	in *agentReadRequest[db.AvatarDeliveryProposalSelector],
) (*agentReadResponse[db.AvatarDeliveryProposalView], error) {
	scope, err := s.avatarDeliveryAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	view, err := s.store.AvatarDeliveryProposal(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, avatarResolutionError(err)
	}
	return agentReadResult(in.Body.OperationID, view), nil
}

func (s *Server) readPendingAvatarDeliveries(
	ctx context.Context,
	in *agentReadRequest[avatarPendingArguments],
) (*agentReadResponse[db.AvatarDeliveryProposalPage], error) {
	scope, err := s.avatarDeliveryAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	page, err := s.store.PendingAvatarDeliveryProposals(ctx, scope, in.Body.Arguments.After, in.Body.Arguments.Limit)
	if err != nil {
		return nil, avatarResolutionError(err)
	}
	return agentReadResult(in.Body.OperationID, page), nil
}

func avatarDeliveryProposalMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return []map[string]any{
		administrativeRead(
			"avatar_delivery_proposal",
			"Recover your original avatar delivery repair proposal by exactly one ope"+
				"ration_id OR proposal_id. Includes original arguments, delivery snapshot"+
				", expiry and any immutable completed result. Pending is not approval; ex"+
				"pired proposals cannot be confirmed. This read does not retry delivery.",
			map[string]any{avatarProposalOperationField: uuidField, "proposal_id": uuidField},
			nil,
			[]string{
				avatarProposalOperationField,
				"proposal_id",
				"arguments",
				"delivery",
				manifestDescription,
				schemaExpiresAt,
				schemaState,
			},
		),
		administrativeRead(
			"avatar_delivery_pending",
			"List your unexpired uncompleted avatar delivery repair proposals, at mos"+
				"t 20 per page. Pass next_cursor as after for the next page; pages can be"+
				" shorter due to the byte budget. Finding a proposal is not consent to re"+
				"try.",
			map[string]any{
				"after": uuidField,
				"limit": map[string]any{
					manifestType:  schemaInteger,
					schemaMinimum: 0,
					schemaMaximum: db.MaxAvatarDeliveryProposalPageSize,
				},
			},
			nil,
			[]string{schemaItems},
		),
	}
}
