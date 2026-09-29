package miniapp

import (
	"embed"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

//go:embed legacyfood.html legacyfood.js legacyfood.css
var foodAssets embed.FS

//go:embed foodphotos/*.jpg
var foodPhotos embed.FS

func (g Gateway) foodRoutes(mux, business *http.ServeMux) {
	mux.HandleFunc("POST /menu", g.legacyFoodPost)
	registerFoodPhotos(mux)
	for path, file := range map[string]string{"/menu": "legacyfood.html", "/miniapp/legacyfood.js": "legacyfood.js", "/miniapp/legacyfood.css": "legacyfood.css"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().
				Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https:; frame-ancestors 'self' https://web.telegram.org")
			g.serveAsset(w, r, foodAssets, file)
		})
	}
	business.HandleFunc("GET /miniapp/api/food", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		view, err := g.API.FoodView(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("pass_key"),
			r.URL.Query().Get("order_id"),
		)
		respond(w, view, err)
	})
	business.HandleFunc("POST /miniapp/api/food", func(w http.ResponseWriter, r *http.Request) {
		var input legacyfood.Command
		if api.Decode(w, r, &input) != nil {
			api.JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidFoodJSON})
			return
		}
		if input.Name != "save_meals" {
			api.JSON(w, http.StatusBadRequest, map[string]string{codeField: "invalid_action"})
			return
		}
		order, err := g.API.FoodCommand(r.Context(), requestOwner(r), input)
		respond(w, order, err)
	})
	business.HandleFunc("POST /miniapp/api/food/quote", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Event string                   `json:"event_id"`
			Meals legacyfood.MealSelection `json:"meals"`
		}
		if api.Decode(w, r, &input) != nil {
			api.JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidFoodJSON})
			return
		}
		quote, err := g.API.FoodQuote(r.Context(), requestOwner(r), input.Event, input.Meals)
		respond(w, quote, err)
	})
}

func registerFoodPhotos(mux *http.ServeMux) {
	entries, _ := foodPhotos.ReadDir("foodphotos")
	for _, entry := range entries {
		name := "foodphotos/" + entry.Name()
		mux.HandleFunc("GET /miniapp/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeFileFS(w, r, foodPhotos, name)
		})
	}
}

const invalidFoodJSON = "invalid_json"
