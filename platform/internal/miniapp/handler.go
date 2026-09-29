// Package miniapp serves the Telegram meal editor through the authenticated application client.
package miniapp

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

//go:embed editor.html editor.js editor.css timetable.html timetable.js timetable.css browser-auth.js
var assets embed.FS

const codeField = "code"

type Gateway struct {
	WebAppURL         string
	Onboarding        func(context.Context, telegram.User) error
	BrowserAuth       *browserauth.Service
	ResolveOrderEvent func(context.Context, string, string) (string, error)
	API               Client
	Token             string
	EventID           string
}

type editorState struct {
	Order    orders.Order `json:"order"`
	Event    orders.Event `json:"event"`
	Editable bool         `json:"editable"`
}

type saveRequest struct {
	Version int64              `json:"version"`
	Key     string             `json:"key"`
	Choice  orders.ChoiceInput `json:"choice"`
}

type ownerKey struct{}

func requestOwner(r *http.Request) string {
	owner, _ := r.Context().Value(ownerKey{}).(string)
	return owner
}

func (g Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	if g.BrowserAuth != nil {
		mux.Handle("/miniapp/auth/", g.BrowserAuth.Handler())
		mux.Handle("/auth", g.BrowserAuth.LegacyHandler())
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /miniapp", func(w http.ResponseWriter, r *http.Request) {
		target := url.URL{Path: r.URL.Path + "/", RawPath: r.URL.EscapedPath() + "/", RawQuery: r.URL.RawQuery}
		address, err := webappurl.Route(g.WebAppURL, "/")
		if err == nil {
			target.Path = strings.TrimSuffix(address.Path, "/") + target.Path
			target.RawPath = strings.TrimSuffix(address.EscapedPath(), "/") + target.RawPath
		}
		http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
	})
	for path, file := range map[string]string{"/miniapp/{$}": "editor.html", "/miniapp/editor.js": "editor.js", "/miniapp/editor.css": "editor.css", "/miniapp/browser-auth.js": "browser-auth.js"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().
				Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; frame-ancestors 'self' https://web.telegram.org")
			g.serveAsset(w, r, assets, file)
		})
	}
	business := http.NewServeMux()
	g.timetableRoutes(mux, business)
	g.foodRoutes(mux, business)
	business.HandleFunc("GET /miniapp/api/orders/{order}", g.read)
	business.HandleFunc("POST /miniapp/api/orders/{order}", g.save)
	business.HandleFunc("POST /miniapp/api/quote", g.quote)
	mux.Handle("/miniapp/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sender, err := g.authenticate(r)
		if err != nil {
			if errors.Is(err, errOnboardingUnavailable) {
				api.JSON(
					w,
					http.StatusServiceUnavailable,
					map[string]string{codeField: "identity_provisioning_unavailable"},
				)
				return
			}
			api.JSON(w, http.StatusUnauthorized, map[string]string{codeField: "invalid_telegram_identity"})
			return
		}
		ctx, owner, err := g.API.AuthenticateTelegram(r.Context(), sender)
		if err != nil {
			api.JSON(w, http.StatusForbidden, map[string]string{codeField: "unknown_user"})
			return
		}
		business.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ownerKey{}, owner)))
	}))
	return browserauth.GuardRouting(webappurl.SubtreeRedirects(g.WebAppURL, mux))
}

func (g Gateway) authenticate(r *http.Request) (int64, error) {
	if r.Header.Get("Authorization") != "" {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "tma ")
		user, err := telegram.VerifyWebApp(raw, g.Token, time.Now())
		if !ok || err != nil {
			return 0, browserauth.ErrIdentity
		}
		if err = g.onboardVerified(r.Context(), user); err != nil {
			return 0, err
		}
		return user.ID, nil
	}
	if g.BrowserAuth == nil {
		return 0, browserauth.ErrIdentity
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !g.BrowserAuth.SameOrigin(r) {
		return 0, browserauth.ErrIdentity
	}
	return g.BrowserAuth.ResolveSession(r)
}

func (g Gateway) read(w http.ResponseWriter, r *http.Request) {
	var resolveErr error
	g, resolveErr = g.forOrder(r)
	if resolveErr != nil {
		respond(w, nil, resolveErr)
		return
	}
	owner := requestOwner(r)
	order, err := g.API.Order(r.Context(), owner, g.EventID, r.PathValue("order"))
	if err != nil {
		respond(w, nil, err)
		return
	}
	event, err := g.API.OrderEvent(r.Context(), owner, g.EventID)
	respond(
		w,
		editorState{
			Order:    order,
			Event:    event,
			Editable: (order.State == "unpaid" || order.State == "cash") && time.Now().Before(event.Deadline),
		},
		err,
	)
}

func (g Gateway) quote(w http.ResponseWriter, r *http.Request) {
	var resolveErr error
	g, resolveErr = g.forOrder(r)
	if resolveErr != nil {
		respond(w, nil, resolveErr)
		return
	}
	var input orders.ChoiceInput
	if api.DecodeOrderRequest(w, r, &input) != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{codeField: "invalid_json"})
		return
	}
	assembleCustomer(&input)
	value, err := g.API.QuoteOrder(r.Context(), requestOwner(r), g.EventID, input)
	respond(w, value, err)
}

func (g Gateway) save(w http.ResponseWriter, r *http.Request) {
	var resolveErr error
	g, resolveErr = g.forOrder(r)
	if resolveErr != nil {
		respond(w, nil, resolveErr)
		return
	}
	var input saveRequest
	if api.DecodeOrderRequest(w, r, &input) != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{codeField: "invalid_json"})
		return
	}
	assembleCustomer(&input.Choice)
	value, err := g.API.ExecuteOrder(r.Context(), requestOwner(r), orders.Command{
		EventID: g.EventID, OrderID: r.PathValue("order"), Name: "edit", Origin: "manual",
		Version: input.Version, Key: input.Key, Choice: &input.Choice,
	})
	respond(w, value, err)
}

func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		api.JSON(w, http.StatusOK, value)
		return
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok {
		api.JSON(w, problem.Status, map[string]string{codeField: problem.Code})
		return
	}
	api.JSON(w, http.StatusServiceUnavailable, map[string]string{codeField: "service_unavailable"})
}

func assembleCustomer(choice *orders.ChoiceInput) {
	choice.FirstName = strings.TrimSpace(choice.FirstName)
	choice.LastName = strings.TrimSpace(choice.LastName)
	choice.Patronymic = strings.TrimSpace(choice.Patronymic)
	parts := []string{choice.FirstName, choice.Patronymic, choice.LastName}
	choice.Customer = strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), " ")
}

func (g Gateway) forOrder(r *http.Request) (Gateway, error) {
	id := r.PathValue("order")
	if id == "" {
		id = r.URL.Query().Get("order_id")
	}
	if g.ResolveOrderEvent == nil || id == "" {
		return g, nil
	}
	event, err := g.ResolveOrderEvent(r.Context(), requestOwner(r), id)
	g.EventID = event
	return g, err
}
