package miniapp

import (
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

// HTML entry routes share the configured public asset path, including legacy routes.
func (g Gateway) serveAsset(w http.ResponseWriter, r *http.Request, files fs.FS, name string) {
	if !strings.HasSuffix(name, ".html") {
		http.ServeFileFS(w, r, files, name)
		return
	}
	page, err := template.ParseFS(files, name)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, struct{ MiniAppPath string }{webappurl.Path(g.WebAppURL, "/miniapp/")})
}
