package sandbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (f *Fake) miniAppRoutes(mux *http.ServeMux) {
	if f.MiniAppURL != "" {
		target, err := url.Parse(f.MiniAppURL)
		if err == nil && target.Host != "" && (target.Scheme == "http" || target.Scheme == "https") {
			proxy := httputil.NewSingleHostReverseProxy(target)
			for _, pattern := range []string{"GET /miniapp/{$}", "GET /miniapp/editor.js", "GET /miniapp/editor.css",
				"GET /miniapp/massage", "GET /massage_timetable", "GET /miniapp/timetable.js", "GET /miniapp/timetable.css",
				"GET /miniapp/api/massage/timetable", "GET /miniapp/browser-auth.js",
				"GET /menu", "POST /menu", "GET /miniapp/legacyfood.js", "GET /miniapp/legacyfood.css",
				"GET /miniapp/foodphotos/{photo}", "GET /miniapp/api/food", "POST /miniapp/api/food", "POST /miniapp/api/food/quote",
				"GET /miniapp/api/orders/{order}", "POST /miniapp/api/orders/{order}", "POST /miniapp/api/quote"} {
				mux.Handle(pattern, proxy)
			}
		}
	}
	mux.HandleFunc("POST /lab/webapp", f.launchMiniApp)
}

func (f *Fake) launchMiniApp(w http.ResponseWriter, r *http.Request) {
	if !labRequest(w, r) {
		return
	}
	var input struct {
		User      int64  `json:"user"`
		MessageID int64  `json:"message_id"`
		URL       string `json:"url"`
	}
	if api.Decode(w, r, &input) != nil {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	if _, known := identity.Subject(input.User); !known {
		api.JSON(w, http.StatusBadRequest, nil)
		return
	}
	f.mu.Lock()
	allowed := f.hasWebAppButton(input.User, input.MessageID, input.URL)
	f.mu.Unlock()
	address, err := url.Parse(input.URL)
	if !allowed || err != nil || !slices.Contains([]string{"/miniapp/", "/miniapp/massage", "/menu"}, address.Path) {
		api.JSON(w, http.StatusForbidden, nil)
		return
	}
	fragment := url.Values{
		"tgWebAppData": {WebAppInitData(telegram.User{ID: input.User, FirstName: "Sandbox"}, f.Token, time.Now())},
	}
	api.JSON(
		w,
		http.StatusOK,
		map[string]string{"url": address.Path + "?" + address.RawQuery + "#" + fragment.Encode()},
	)
}

func (f *Fake) hasWebAppButton(user, message int64, address string) bool {
	for _, msg := range f.messages {
		if msg.Chat.ID != user || msg.ID != message {
			continue
		}
		for _, row := range msg.Markup.Rows {
			for _, button := range row {
				if button.WebApp != nil && button.WebApp.URL == address {
					return true
				}
			}
		}
	}
	return false
}

// WebAppInitData is a Telegram launch fixture, never a production identity issuer.
func WebAppInitData(user telegram.User, token string, now time.Time) string {
	encoded, _ := json.Marshal(user)
	values := url.Values{
		"user":      {string(encoded)},
		"auth_date": {strconv.FormatInt(now.Unix(), 10)},
		"query_id":  {"sandbox-launch"},
	}
	fields := make([]string, 0, len(values))
	for key, items := range values {
		fields = append(fields, key+"="+items[0])
	}
	slices.Sort(fields)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(fields, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}
