package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type pairInvitationArguments struct {
	EventID      uuid.UUID `json:"event_id"`
	InvitationID uuid.UUID `json:"invitation_id"`
}

func (s *Server) registerPairReads(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:  "agent-pair-invitation",
		Method:       http.MethodPost,
		Path:         "/internal/agent/registrations/invitation",
		MaxBodyBytes: internalBodyLimit,
	}, s.agentPairInvitation)
}

func (s *Server) agentPairInvitation(
	ctx context.Context,
	in *agentReadRequest[pairInvitationArguments],
) (*agentReadResponse[registrations.Invitation], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	result, err := s.store.PairInvitation(ctx, scope, args.EventID, args.InvitationID)
	switch {
	case errors.Is(err, db.ErrPairDenied), errors.Is(err, pgx.ErrNoRows):
		return nil, huma.Error404NotFound("invitation not found")
	case err != nil:
		return nil, registrationError(err)
	default:
		return agentReadResult(in.Body.OperationID, result), nil
	}
}

func pairInvitationMethod() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	return map[string]any{
		"id":                "invitation",
		manifestDescription: "Read pair consent metadata by event_id and invitation_id. Only the inviter or intended target can read it; administrator rights do not override consent privacy. No partner pass, payment or account profile is included. Pending state alone does not mean the invitation is still within expires_at. Display instants in the event timezone.",
		manifestPermissions: []string{},
		manifestPath:        "/internal/agent/registrations/invitation",
		manifestEffect:      effectRead,
		manifestTimeout:     toolTimeoutMillis,
		manifestMaxResult:   toolResultLimit,
		manifestInputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaEventID, "invitation_id"},
			schemaProperties:           map[string]any{schemaEventID: uuidField, "invitation_id": uuidField},
		},
		manifestOutputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaProperties: map[string]any{
				"id":                      uuidField,
				schemaEventID:             uuidField,
				"inviter_registration_id": uuidField,
				"target_user_id":          uuidField,
				schemaState: map[string]any{
					manifestType: schemaString,
					schemaEnum:   []string{"pending", "accepted", "declined", "revoked", "expired"},
				},
				schemaVersion:   map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
				schemaCreatedAt: instant,
				schemaExpiresAt: instant,
				"decided_at":    instant,
				"pair_id":       uuidField,
			},
			schemaRequired: []string{
				"id",
				schemaEventID,
				"inviter_registration_id",
				"target_user_id",
				schemaState,
				schemaVersion,
				schemaCreatedAt,
				"expires_at",
			},
		},
		manifestIdempotency:  false,
		manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
		manifestMIMETypes:    []string{},
		manifestDisclosure:   false,
	}
}
