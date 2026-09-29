package miniapp

import (
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

type timetableState struct {
	Owner    string           `json:"owner"`
	Calendar massage.Calendar `json:"calendar"`
}

func (g Gateway) timetableRoutes(mux, business *http.ServeMux) {
	for path, file := range map[string]string{
		"/massage_timetable": "timetable.html", "/miniapp/massage": "timetable.html",
		"/miniapp/timetable.js": "timetable.js", "/miniapp/timetable.css": "timetable.css",
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set(
				"Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'self' https://web.telegram.org",
			)
			g.serveAsset(w, r, assets, file)
		})
	}
	business.HandleFunc("GET /miniapp/api/massage/timetable", g.timetable)
}

// The shared Mini App middleware verifies initData. The domain API independently
// checks current event specialist/admin rights before returning any client data.
func (g Gateway) timetable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	event := r.URL.Query().Get("event")
	if event == "" {
		event = g.EventID
	}
	owner := requestOwner(r)
	calendar, err := g.API.MassageTimetable(r.Context(), owner, event)
	respond(w, timetableState{Owner: owner, Calendar: calendar}, err)
}
