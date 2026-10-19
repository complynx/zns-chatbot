// Package api exposes Telegram intake and authorized agent and administrative operations.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/miniapp"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// Acceptor commits both the accepted Telegram update and its queue job.
type Acceptor interface {
	Accept(context.Context, telegram.Update, json.RawMessage) (db.Task, bool, error)
}

const (
	webhookBodyLimit  = 1 << 20
	internalBodyLimit = 8192
	toolTimeoutMillis = 5000
	toolResultLimit   = 16384
)

type Options struct {
	MiniAppAllowLocalHTTP                                                     bool
	RegistrationDetails                                                       *registrationdetails.Protector
	TelegramPrelink                                                           TelegramPrelinkOptions
	MiniAppOrigin, MiniAppBotToken                                            string
	AvatarRenderer                                                            avatar.Processor
	Telegram                                                                  *telegram.Client
	BotID                                                                     int64
	WebhookSecret, OrganizationID, ActorID, Issuer, LoomAudience, APIAudience string
}

type Server struct {
	store             *db.Store
	iam               *identity.Client
	tasks             Acceptor
	options           Options
	public, internal  *http.ServeMux
	accepting         atomic.Bool
	settingsMu        sync.Mutex
	settingsTTL       time.Duration
	settingsTTLKnown  bool
	archiveValidation chan struct{}
	miniAppKey        miniapp.SessionKey
	prelinkVerifier   *identity.TelegramPrelinkVerifier
	prelinkBearerHash [sha256.Size]byte
}

func New(store *db.Store, iam *identity.Client, tasks Acceptor, options Options) (*Server, error) {
	if store == nil || iam == nil || tasks == nil || options.BotID <= 0 || options.WebhookSecret == "" ||
		options.OrganizationID == "" ||
		options.ActorID == "" ||
		options.Issuer == "" ||
		options.LoomAudience == "" {
		return nil, errors.New("API store, identity, intake and identity options required")
	}
	s := &Server{
		store:             store,
		iam:               iam,
		tasks:             tasks,
		options:           options,
		public:            http.NewServeMux(),
		internal:          http.NewServeMux(),
		archiveValidation: make(chan struct{}, 1),
	}
	if options.MiniAppOrigin != "" {
		prefix := strconv.FormatInt(options.BotID, 10) + ":"
		if !miniapp.ValidSessionOrigin(options.MiniAppOrigin, options.MiniAppAllowLocalHTTP) ||
			!strings.HasPrefix(options.MiniAppBotToken, prefix) ||
			len(options.MiniAppBotToken) <= len(prefix) {
			return nil, errors.New("mini app trusted origin and bot token required")
		}
		s.miniAppKey = miniapp.NewSessionKey(options.MiniAppBotToken)
		s.registerMiniApp()
	}
	if err := s.registerTelegramPrelink(); err != nil {
		return nil, err
	}
	s.accepting.Store(true)
	s.public.HandleFunc("POST /telegram/webhook", s.webhook)
	s.public.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	s.public.HandleFunc("GET /readyz", s.ready)
	s.internal.HandleFunc("GET /internal/agent/manifest", s.manifest)
	internalConfig := huma.DefaultConfig("Zinusya internal API", "0.1.0")
	internalConfig.Transformers = append(internalConfig.Transformers, registrationDetailsError)
	api := humago.New(s.internal, internalConfig)
	s.register(api)
	s.registerFiles(api)
	return s, nil
}
func (s *Server) PublicHandler() http.Handler   { return s.public }
func (s *Server) InternalHandler() http.Handler { return http.HandlerFunc(s.serveInternal) }

// SetAccepting stops new intake before worker shutdown.
func (s *Server) SetAccepting(accepting bool) { s.accepting.Store(accepting) }

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if !s.accepting.Load() || s.store.Pool().Ping(ctx) != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")),
		[]byte(s.options.WebhookSecret),
	) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.accepting.Load() {
		http.Error(w, "not accepting", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookBodyLimit))
	if err != nil {
		if _, isLimit := errors.AsType[*http.MaxBytesError](err); isLimit {
			http.Error(w, "update too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid update", http.StatusBadRequest)
		}
		return
	}
	var update telegram.Update
	if json.Unmarshal(body, &update) != nil || update.ID < 0 || !privateUpdate(update) {
		http.Error(w, "invalid private update", http.StatusBadRequest)
		return
	}
	t, duplicate, err := s.tasks.Accept(r.Context(), update, body)
	if errors.Is(err, task.ErrUnsupportedUpdate) {
		writeJSON(w, http.StatusOK, map[string]any{"ignored": true, schemaReason: "unsupported_update"})
		return
	}
	if err != nil {
		http.Error(w, "intake unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{schemaTaskID: t.ID, "duplicate": duplicate})
}
func privateUpdate(u telegram.Update) bool {
	valid := func(user telegram.User, chat telegram.Chat) bool {
		return user.ID > 0 && user.ID < 1<<52 && !user.IsBot && chat.Type == "private" && chat.ID == user.ID
	}
	if u.Message != nil && u.CallbackQuery == nil && u.Message.From != nil {
		return valid(*u.Message.From, u.Message.Chat)
	}
	if u.CallbackQuery != nil && u.Message == nil && u.CallbackQuery.Message != nil {
		return valid(u.CallbackQuery.From, u.CallbackQuery.Message.Chat)
	}
	return false
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type AuthInput struct {
	Authorization string `header:"Authorization"`
}

func (s *Server) authorizeIdentity(ctx context.Context, header string, admin bool) (identity.Principal, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return identity.Principal{}, huma.Error401Unauthorized("bearer token required")
	}
	p, err := s.iam.Verify(ctx, token)
	if err != nil {
		if errors.Is(err, identity.ErrUnavailable) {
			return p, huma.Error503ServiceUnavailable("identity unavailable")
		}
		return p, huma.Error401Unauthorized("invalid identity")
	}
	if p.OrganizationID != s.options.OrganizationID || p.Subject == s.options.ActorID {
		return p, huma.Error403Forbidden("user identity required")
	}
	if admin {
		if _, hasRole := p.Roles["zinusya.admin"][s.options.OrganizationID]; !hasRole {
			return p, huma.Error403Forbidden("administrator role required")
		}
	}
	return p, nil
}

func (s *Server) authorize(ctx context.Context, header string, admin bool) (identity.Principal, error) {
	p, err := s.authorizeIdentity(ctx, header, admin)
	if err != nil {
		return p, err
	}
	u, err := s.store.GetUserBySubject(ctx, s.options.BotID, s.options.OrganizationID, p.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, huma.Error403Forbidden("local account required")
	}
	if err != nil {
		return p, huma.Error503ServiceUnavailable("account unavailable")
	}
	if u.Revoked || u.Organization != s.options.OrganizationID {
		return p, huma.Error403Forbidden("account access denied")
	}
	return p, nil
}
func (s *Server) account(ctx context.Context, p identity.Principal) (db.User, error) {
	u, err := s.store.GetUserBySubject(ctx, s.options.BotID, s.options.OrganizationID, p.Subject)
	if err != nil {
		return u, huma.Error503ServiceUnavailable("account unavailable")
	}
	return u, nil
}
