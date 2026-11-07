package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

//nolint:dupl // Keep the specialist adapter typed to its private instant arguments.
func (s *Server) miniAppMassageInstant(w http.ResponseWriter, r *http.Request) {
	handleMiniAppMassageWrite(s, w, r,
		func(args db.MassageInstantArguments) uuid.UUID { return args.EventID },
		func(ctx context.Context, session db.MiniAppMassageSession,
			input miniAppMassageWriteInput[db.MassageInstantArguments], prepare bool,
		) (any, error) {
			if prepare {
				if input.ProposalID.UUID != uuid.Nil {
					return s.store.AdoptMiniAppMassageInstant(ctx, s.eventScope(), session,
						input.OperationID, input.ProposalID.UUID, input.Arguments)
				}
				return s.store.PrepareMiniAppMassageInstant(ctx, s.eventScope(), session,
					input.OperationID, input.Arguments)
			}
			return s.store.BookMiniAppMassageInstant(ctx, s.eventScope(), session,
				input.OperationID, input.ProposalID.UUID, input.Arguments)
		})
}
