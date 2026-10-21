package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type miniAppOrderWrites struct {
	*Server

	prepare func(context.Context, db.EventScope, db.MiniAppOrderSession, uuid.UUID, db.OrderSubmission) (db.OrderPreparation, error)
	submit  func(context.Context, db.EventScope, db.MiniAppOrderSession, uuid.UUID, db.OrderSubmission, uuid.UUID) (db.OrderSubmissionResult, error)
	cancel  func(context.Context, db.EventScope, db.MiniAppOrderSession, uuid.UUID, db.OrderCancellation) (db.SubmittedOrder, error)
	failure func(context.Context, http.ResponseWriter, *http.Request, error)
}

func (s *miniAppOrderWrites) register(path string) {
	s.public.HandleFunc("POST "+path+"submit/prepare", s.miniAppPrepareOrder)
	s.public.HandleFunc("POST "+path+"submit", s.miniAppSubmitOrder)
	s.public.HandleFunc("POST "+path+"cancel", s.miniAppCancelOrder)
}

func (s *miniAppOrderWrites) miniAppOrderResult(ctx context.Context, w http.ResponseWriter,
	r *http.Request, result any, err error,
) {
	if err != nil {
		s.failure(ctx, w, r, err)
		return
	}
	s.Server.miniAppOrderResult(ctx, w, r, result, nil)
}

func (s *Server) registerMiniAppMealSubmission() {
	writes := miniAppOrderWrites{Server: s, prepare: s.store.PrepareMiniAppMealOrderSubmission,
		submit: s.store.SubmitMiniAppMealOrder, cancel: s.store.CancelMiniAppMealOrder, failure: s.miniAppMealsError}
	writes.register("/app/meals/{event}/")
	s.public.HandleFunc("GET /app/meals/{event}/submitted", s.miniAppSubmittedMeals)
	s.public.HandleFunc("GET /app/meals/{event}/submitted/{order}", s.miniAppSubmittedMeals)
	s.public.HandleFunc("GET /app/meals/{event}/operation-receipts/{kind}/{operation}", s.miniAppSubmittedMeals)
}
