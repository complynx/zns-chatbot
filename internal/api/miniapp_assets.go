package api

import (
	"embed"
	"net/http"
)

//go:embed miniapp_ui/*
var miniAppAssets embed.FS

func (s *Server) miniAppPage(w http.ResponseWriter, r *http.Request) {
	file, contentType := miniAppAsset(r.URL.Path)
	if file == "" {
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

// miniAppAsset keeps public asset lookup limited to the explicit route list.
func miniAppAsset(path string) (string, string) {
	const javaScriptType = "text/javascript; charset=utf-8"
	switch path {
	case "/app/":
		return "index.html", "text/html; charset=utf-8"
	case "/app/app.js":
		return "app.js", javaScriptType
	case "/app/order-proof.js":
		return "order-proof.js", javaScriptType
	case "/app/pair-assignments.js":
		return "pair-assignments.js", javaScriptType
	case "/app/assignments.js":
		return "assignments.js", javaScriptType
	case "/app/avatar-admin.js":
		return "avatar-admin.js", javaScriptType
	case "/app/pairs.js":
		return "pairs.js", javaScriptType
	case "/app/avatars.js":
		return "avatars.js", javaScriptType
	case "/app/crop.js":
		return "crop.js", javaScriptType
	case "/app/crop.css":
		return "crop.css", "text/css; charset=utf-8"
	case "/app/meals.js":
		return "meals.js", javaScriptType
	case "/app/orders.js":
		return "orders.js", javaScriptType
	case "/app/order-reviews.js":
		return "order-reviews.js", javaScriptType
	case "/app/pass-payments.js":
		return "pass-payments.js", javaScriptType
	case "/app/payment-reviews.js":
		return "payment-reviews.js", javaScriptType
	case "/app/massage.js":
		return "massage.js", javaScriptType
	case "/app/massage-admin.js":
		return "massage-admin.js", javaScriptType
	case "/app/massage-staff.js":
		return "massage-staff.js", javaScriptType
	case "/app/knowledge.js":
		return "knowledge.js", javaScriptType
	case "/app/app.css":
		return "app.css", "text/css; charset=utf-8"
	default:
		return "", ""
	}
}
