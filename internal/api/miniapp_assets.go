package api

import (
	"embed"
	"net/http"
)

//go:embed miniapp_ui/*
var miniAppAssets embed.FS

func (s *Server) miniAppPage(w http.ResponseWriter, r *http.Request) {
	const javaScriptType = "text/javascript; charset=utf-8"
	var file, contentType string
	switch r.URL.Path {
	case "/app/":
		file, contentType = "index.html", "text/html; charset=utf-8"
	case "/app/app.js":
		file, contentType = "app.js", javaScriptType
	case "/app/order-proof.js":
		file, contentType = "order-proof.js", javaScriptType
	case "/app/assignments.js":
		file, contentType = "assignments.js", javaScriptType
	case "/app/avatar-admin.js":
		file, contentType = "avatar-admin.js", javaScriptType
	case "/app/pairs.js":
		file, contentType = "pairs.js", javaScriptType
	case "/app/avatars.js":
		file, contentType = "avatars.js", javaScriptType
	case "/app/meals.js":
		file, contentType = "meals.js", javaScriptType
	case "/app/orders.js":
		file, contentType = "orders.js", javaScriptType
	case "/app/order-reviews.js":
		file, contentType = "order-reviews.js", javaScriptType
	case "/app/payment-reviews.js":
		file, contentType = "payment-reviews.js", javaScriptType
	case "/app/massage.js":
		file, contentType = "massage.js", javaScriptType
	case "/app/massage-admin.js":
		file, contentType = "massage-admin.js", javaScriptType
	case "/app/massage-staff.js":
		file, contentType = "massage-staff.js", javaScriptType
	case "/app/app.css":
		file, contentType = "app.css", "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	raw, err := miniAppAssets.ReadFile("miniapp_ui/" + file)
	if err != nil {
		http.Error(w, "page unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().
		Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self'; connect-src 'self'; img-src 'self' blob:; base-uri 'none'; object-src 'none'; form-action 'none'; frame-ancestors https://web.telegram.org")
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}
