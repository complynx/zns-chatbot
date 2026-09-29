package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func creditsRoutes(mux *http.ServeMux, service credits.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/credits/aggregate", func(w http.ResponseWriter, r *http.Request) {
		period, err := time.Parse("2006-01", r.URL.Query().Get("month"))
		if err != nil {
			respond(logger, w, nil, creditProblem(credits.ErrInvalid))
			return
		}
		value, err := service.Aggregate(r.Context(), requestOwner(r), period, r.URL.Query().Get("cursor"))
		respond(logger, w, value, creditProblem(err))
	})
	mux.HandleFunc("GET /v1/credits/permissions", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Permissions(r.Context(), requestOwner(r))
		respond(logger, w, value, creditProblem(err))
	})
	mux.HandleFunc("GET /v1/credits/default", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.DefaultPolicy(r.Context(), requestOwner(r))
		respond(logger, w, value, creditProblem(err))
	})
	mux.HandleFunc("POST /v1/credits/default", func(w http.ResponseWriter, r *http.Request) {
		var input credits.PolicyChange
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.SetPolicy(r.Context(), requestOwner(r), "*", input)
		respond(logger, w, value, creditProblem(err))
	})
	for _, route := range []struct {
		path  string
		payer func(*http.Request) string
	}{
		{"/v1/credits", requestOwner}, {"/v1/credits/users/{owner}", func(r *http.Request) string { return r.PathValue("owner") }},
	} {
		mux.HandleFunc("GET "+route.path+"/usage", func(w http.ResponseWriter, r *http.Request) {
			value, err := service.Usage(r.Context(), requestOwner(r), route.payer(r))
			respond(logger, w, value, creditProblem(err))
		})
		mux.HandleFunc("GET "+route.path+"/history", func(w http.ResponseWriter, r *http.Request) {
			value, err := service.History(r.Context(), requestOwner(r), route.payer(r), r.URL.Query().Get("cursor"))
			respond(logger, w, value, creditProblem(err))
		})
	}
	mux.HandleFunc("POST /v1/credits/users/{owner}/policy", func(w http.ResponseWriter, r *http.Request) {
		var input credits.PolicyChange
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.SetPolicy(r.Context(), requestOwner(r), r.PathValue("owner"), input)
		respond(logger, w, value, creditProblem(err))
	})
}

func creditProblem(err error) error {
	switch {
	case errors.Is(err, credits.ErrInvalid):
		return &core.ProblemError{Status: http.StatusBadRequest, Code: "credits_invalid"}
	case errors.Is(err, credits.ErrConflict):
		return &core.ProblemError{Status: http.StatusConflict, Code: "credits_conflict"}
	default:
		return err
	}
}
