package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const internalError = "internal_error"

// knowledgeRoutes must be registered inside the authenticated business mux.
// Assessment is deliberately absent: only the trusted host classifier calls it.
func knowledgeRoutes(mux *http.ServeMux, service knowledge.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/knowledge/capabilities", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Capabilities(r.Context(), requestOwner(r))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge/scope", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Scope(r.Context(), requestOwner(r), r.URL.Query().Get("event"))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge/scopes/page", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ScopePage(r.Context(), requestOwner(r), r.URL.Query().Get("cursor"))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge/page", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		value, err := service.RetrievePage(
			r.Context(),
			requestOwner(r),
			knowledge.Query{Event: q.Get("event"), Topic: q.Get("topic"), Text: q.Get("q"), Cursor: q.Get("cursor")},
		)
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge/fact", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		value, err := service.Fact(r.Context(), requestOwner(r), q.Get("event"), q.Get("topic"), q.Get("key"))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge/scopes", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Scopes(r.Context(), requestOwner(r))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		value, err := service.Retrieve(
			r.Context(),
			requestOwner(r),
			knowledge.Query{Event: q.Get("event"), Topic: q.Get("topic"), Text: q.Get("q")},
		)
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/knowledge/proposals", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var after int64
		var err error
		if q.Get("after") != "" {
			after, err = strconv.ParseInt(q.Get("after"), 10, 64)
		}
		if err != nil || (q.Get("review") != "" && q.Get("review") != "true" && q.Get("review") != "false") {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Proposals(
			r.Context(),
			requestOwner(r),
			knowledge.ProposalQuery{Event: q.Get("event"), After: after, ReviewQueue: q.Get("review") == "true"},
		)
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/me/memos", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Memos(r.Context(), requestOwner(r))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/me/memos/{key}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Memo(r.Context(), requestOwner(r), r.PathValue("key"))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/knowledge/actions", func(w http.ResponseWriter, r *http.Request) {
		var command knowledge.Command
		if decodeKnowledgeCommand(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Execute(r.Context(), requestOwner(r), command)
		respondKnowledge(logger, w, value, err)
	})
}

func respondKnowledge(logger *slog.Logger, w http.ResponseWriter, value any, err error) {
	if err == nil {
		JSON(w, http.StatusOK, value)
		return
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok {
		JSON(w, problem.Status, problem)
		return
	}
	// PostgreSQL diagnostics can contain rejected private memo text.
	logger.Error("Knowledge request failed")
	JSON(w, http.StatusInternalServerError, map[string]string{codeField: internalError})
}
