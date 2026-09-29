package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// memoryRoutes belongs inside the authenticated business mux.
func memoryRoutes(mux *http.ServeMux, service knowledge.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/memory/deletions", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.MemoryDeletions(r.Context(), requestOwner(r))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/memory/document", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.DocumentState(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("topic"),
			r.URL.Query().Get("key"),
		)
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/memory/summary", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.MemorySummary(r.Context(), requestOwner(r), memoryQuery(r))
		respondKnowledge(logger, w, value, err)
	})
	for _, path := range []string{"GET /v1/memory/index", "GET /v1/memory/search"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			value, err := service.SearchMemory(r.Context(), requestOwner(r), memoryQuery(r))
			respondKnowledge(logger, w, value, err)
		})
	}
	mux.HandleFunc("GET /v1/memory/read", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ReadMemoryPage(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("ref"),
			r.URL.Query().Get("cursor"),
		)
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/memory/revision", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ReadMemoryRevisionPage(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("ref"),
			r.URL.Query().Get("cursor"),
		)
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/memory/sources", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.MemorySources(r.Context(), requestOwner(r), r.URL.Query().Get("ref"))
		respondKnowledge(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/memory/history", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.MemoryHistory(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("ref"),
			r.URL.Query().Get("cursor"),
		)
		respondKnowledge(logger, w, value, err)
	})
}

func memoryQuery(r *http.Request) knowledge.MemoryQuery {
	q := r.URL.Query()
	return knowledge.MemoryQuery{
		Namespace: q.Get("namespace"),
		Event:     q.Get("event"),
		Topic:     q.Get("topic"),
		Text:      q.Get("q"),
		Mode:      q.Get("mode"),
		Cursor:    q.Get("cursor"),
	}
}
