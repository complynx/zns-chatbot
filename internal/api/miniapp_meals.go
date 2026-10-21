package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

type miniAppMealDraftInput struct {
	OperationID            uuid.UUID              `json:"operation_id"`
	ExpectedEventVersion   *int64                 `json:"expected_event_version"`
	ExpectedCatalogVersion *int64                 `json:"expected_catalog_version"`
	ExpectedDraftVersion   *int64                 `json:"expected_draft_version"`
	Choices                []orders.MealDayChoice `json:"choices"`
}

func (s *Server) registerMiniAppMeals() {
	s.registerMiniAppMealSubmission()
	s.public.HandleFunc("GET /app/meals/events", s.miniAppMealsEvents)
	s.public.HandleFunc("GET /app/meals/{event}/catalog", s.miniAppMealsRead)
	s.public.HandleFunc("GET /app/meals/{event}/draft", s.miniAppMealsRead)
	s.public.HandleFunc("GET /app/meals/{event}/draft/operations/{operation}", s.miniAppMealsRead)
	s.public.HandleFunc("POST /app/meals/{event}/draft", s.miniAppMealsSave)
}

func (s *Server) miniAppMealsEvents(w http.ResponseWriter, r *http.Request) {
	s.miniAppEventList(w, r, s.store.ListOwnMealDraftEvents, s.miniAppMealsError)
}

func (s *Server) miniAppMealsRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid meals event")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	scope := miniAppOrdersScope(attempt)
	var result any
	switch {
	case r.PathValue("operation") != "":
		operation, parseErr := uuid.Parse(r.PathValue("operation"))
		if parseErr != nil || operation == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid meal operation")
			return
		}
		result, err = s.store.OwnMealDraftOperation(ctx, scope, event, operation)
	case strings.HasSuffix(r.URL.Path, "/catalog"):
		version := int64(0)
		if raw := r.URL.Query().Get("version"); raw != "" {
			version, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || version < 0 {
				miniAppHTTPError(w, http.StatusBadRequest, "invalid meal catalog version")
				return
			}
		}
		result, err = s.store.OwnMealCatalog(ctx, scope, event, version)
	default:
		result, err = s.store.OwnMealDraft(ctx, scope, event)
	}
	if err != nil {
		s.miniAppMealsError(ctx, w, r, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// decodeMiniAppMeals requires one bounded JSON object and rejects caller prices,
// owner IDs, unknown fields and trailing content before any business write.
func decodeMiniAppMeals(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid meals request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid meals request")
		return false
	}
	return true
}

func (s *Server) miniAppMealsSave(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid meals event")
		return
	}
	var input miniAppMealDraftInput
	if !decodeMiniAppMeals(w, r, &input) {
		return
	}
	if input.ExpectedEventVersion == nil || *input.ExpectedEventVersion < 1 ||
		input.ExpectedCatalogVersion == nil || *input.ExpectedCatalogVersion < 1 ||
		input.Choices == nil || input.ExpectedDraftVersion == nil ||
		*input.ExpectedDraftVersion < 0 ||
		input.OperationID == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit operation, versions and choices required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	if _, err = s.authenticatedMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.SaveMiniAppMealDraft(
		ctx,
		db.EventScope{BotID: s.options.BotID, Organization: s.options.OrganizationID},
		bearer,
		s.miniAppKey.Generation(),
		db.MealDraftChange{
			OperationID:            input.OperationID,
			EventID:                event,
			ExpectedEventVersion:   *input.ExpectedEventVersion,
			ExpectedCatalogVersion: *input.ExpectedCatalogVersion,
			ExpectedDraftVersion:   *input.ExpectedDraftVersion,
			Choices:                input.Choices,
		},
	)
	if err != nil {
		s.miniAppMealsError(ctx, w, r, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) miniAppMealsError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		if _, sessionErr := s.currentMiniApp(ctx, r); sessionErr != nil {
			miniAppError(w, sessionErr)
			return
		}
		miniAppHTTPError(w, http.StatusNotFound, "meal event, draft operation or access not found")
		return
	}
	switch {
	case errors.Is(err, db.ErrMealDeadlineMissing):
		miniAppHTTPError(w, http.StatusConflict, "meal payment deadline is not configured")
	case errors.Is(err, db.ErrEmptyMealOrder):
		miniAppHTTPError(w, http.StatusBadRequest, "meal order has no selections")
	case errors.Is(err, db.ErrConflict),
		errors.Is(err, orders.ErrSalesClosed):
		miniAppHTTPError(
			w,
			http.StatusConflict,
			"meal draft or catalog changed, or affected sales closed; read current state",
		)
	case errors.Is(err, orders.ErrMealChoices),
		errors.Is(err, orders.ErrMealCatalog),
		errors.Is(err, orders.ErrAmount),
		errors.Is(err, orders.ErrSelection):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid meal choices or amount exceeds supported bounds")
	default:
		miniAppError(w, err)
	}
}
