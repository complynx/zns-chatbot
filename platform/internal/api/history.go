package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

const invalidHistory = "history_invalid"

func historyRoutes(mux *http.ServeMux, service conversation.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/me/history/generation", func(w http.ResponseWriter, r *http.Request) {
		generation, err := service.Generation(r.Context(), requestOwner(r))
		respond(logger, w, map[string]int64{"generation": generation}, err)
	})
	mux.HandleFunc("GET /v1/me/history/{id}/text", func(w http.ResponseWriter, r *http.Request) {
		id, e1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
		offset, e2 := historyInteger(r, "offset", 0)
		limit, e3 := historyInteger(r, "limit", conversation.MaxChunkCharacters)
		if e1 != nil || e2 != nil || e3 != nil || offset < 0 || offset > conversation.MaxBodyBytes || limit < 1 ||
			limit > conversation.MaxChunkCharacters {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidHistory})
			return
		}
		value, err := service.ReadText(
			r.Context(),
			requestOwner(r),
			id,
			int(offset),
			int(limit),
			r.URL.Query().Get("digest"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/me/history", func(w http.ResponseWriter, r *http.Request) {
		before, e1 := historyInteger(r, "before", 0)
		after, e2 := historyInteger(r, "after", 0)
		limit, e3 := historyInteger(r, "limit", conversation.MaxPage)
		if e1 != nil || e2 != nil || e3 != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidHistory})
			return
		}
		value, err := service.Read(
			r.Context(),
			requestOwner(r),
			conversation.Query{Before: before, After: after, Limit: int(limit)},
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/me/history/context", func(w http.ResponseWriter, r *http.Request) {
		count, err := historyInteger(r, "count", conversation.DefaultRecent)
		if err != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidHistory})
			return
		}
		value, err := service.Window(r.Context(), requestOwner(r), int(count))
		respond(logger, w, value, err)
	})
}

func historyInteger(r *http.Request, key string, fallback int64) (int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
