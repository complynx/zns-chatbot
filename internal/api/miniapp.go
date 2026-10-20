package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/miniapp"
)

const (
	miniAppBodyLimit  = 64 << 10
	miniAppTimeout    = 5 * time.Second
	miniAppAuthScheme = "ZinusyaSession"
)

type miniAppBootstrapRequest struct{ InitData, Nonce string }

type miniAppSessionResponse struct {
	ExpiresAt   time.Time `json:"expires_at"`
	UserID      uuid.UUID `json:"user_id"`
	FullName    string    `json:"full_name"`
	BotLanguage string    `json:"bot_language"`
}

type miniAppBootstrapResponse struct {
	miniAppSessionResponse

	SessionToken string `json:"session_token"`
}

func (s *Server) registerMiniApp() {
	s.registerMiniAppPairs()
	s.registerMiniAppAssignments()
	s.registerMiniAppPairAssignments()
	s.registerMiniAppMassage()
	s.registerMiniAppOrders()
	s.registerMiniAppMeals()
	s.registerMiniAppAvatars()
	s.registerMiniAppPayments()
	s.registerMiniAppPassPayments()
	s.public.HandleFunc("GET /app/", s.miniAppPage)
	s.public.HandleFunc("POST /app/session/bootstrap", s.miniAppBootstrap)
	s.public.HandleFunc("GET /app/me", s.miniAppMe)
	s.public.HandleFunc("GET /app/account", s.miniAppAccount)
	s.public.HandleFunc("GET /app/profile", s.miniAppProfile)
	s.public.HandleFunc("POST /app/profile/name", s.miniAppProfileChange)
	s.public.HandleFunc("POST /app/profile/language", s.miniAppProfileChange)
	s.public.HandleFunc("POST /app/session/logout", s.miniAppLogout)
	if s.options.RegistrationDetails != nil {
		s.public.HandleFunc("GET /app/registration-details", s.miniAppDetails)
		s.public.HandleFunc("GET /app/registration-details/operations/{operation}", s.miniAppDetails)
		s.public.HandleFunc("POST /app/registration-details/legal-name", s.miniAppDetailsChange)
		s.public.HandleFunc("POST /app/registration-details/passport-number", s.miniAppDetailsChange)
	}
}

func (s *Server) miniAppRequest(w http.ResponseWriter, r *http.Request) bool {
	if !s.miniAppOriginRequest(w, r) {
		return false
	}
	if r.Method == http.MethodPost {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			miniAppHTTPError(w, http.StatusUnsupportedMediaType, "JSON request required")
			return false
		}
	}
	return true
}

// miniAppOriginRequest is also used by the narrow bounded image-upload route.
func (s *Server) miniAppOriginRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !s.accepting.Load() {
		miniAppHTTPError(w, http.StatusServiceUnavailable, "application is restarting")
		return false
	}
	origin := r.Header.Get("Origin")
	if (r.Method != http.MethodGet || origin != "") && origin != s.options.MiniAppOrigin {
		miniAppHTTPError(w, http.StatusForbidden, "application origin required")
		return false
	}

	return true
}

func decodeMiniAppBootstrap(reader io.Reader) (miniAppBootstrapRequest, error) {
	var result miniAppBootstrapRequest
	decoder := json.NewDecoder(reader)
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return result, miniapp.ErrInvalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return result, tokenErr
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return result, miniapp.ErrInvalid
		}
		seen[key] = true
		var target *string
		switch key {
		case "init_data":
			target = &result.InitData
		case "bootstrap_nonce":
			target = &result.Nonce
		default:
			return result, miniapp.ErrInvalid
		}
		if err = decoder.Decode(target); err != nil {
			return result, err
		}
	}
	if _, err = decoder.Token(); err != nil {
		return result, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return result, miniapp.ErrInvalid
	}
	return result, nil
}

func (s *Server) miniAppBootstrap(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	input, err := decodeMiniAppBootstrap(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	if err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid authentication request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	view, err := s.bootstrapMiniApp(ctx, input)
	if err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	//nolint:gosec // G117: deliberately return the new app bearer, never an IAM token.
	_ = json.NewEncoder(w).Encode(view)
}

func (s *Server) bootstrapMiniApp(
	ctx context.Context,
	input miniAppBootstrapRequest,
) (miniAppBootstrapResponse, error) {
	nonce, err := miniapp.DecodeNonce(input.Nonce)
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	// Reject forged proofs before any DB access; apply the tighter current policy next.
	proof, err := miniapp.Verify(input.InitData, s.options.MiniAppBotToken, s.options.BotID,
		time.Now(), miniapp.MaxLoginAge, miniapp.LoginFutureSkew)
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	policy, err := settings.Document.MiniAppPolicy()
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	if !proof.AuthDate.Add(policy.MaxAge).After(time.Now()) {
		return miniAppBootstrapResponse{}, miniapp.ErrInvalid
	}
	profile, err := json.Marshal(proof.User)
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	claim := db.MiniAppClaim{
		BotID:          s.options.BotID,
		TelegramUserID: proof.User.ID,
		Organization:   s.options.OrganizationID,
		Profile:        profile,
		ProofHash:      proof.Digest,
		NonceHash:      sha256.Sum256(nonce[:]),
		Generation:     s.miniAppKey.Generation(),
		AuthDate:       proof.AuthDate,
		MaxAge:         policy.MaxAge,
		PendingTTL:     policy.PendingTTL,
		SessionTTL:     policy.SessionTTL,
	}
	attempt, err := s.store.ClaimMiniAppSession(ctx, claim)
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	owner := attempt.User
	if owner.Subject == "" {
		account, ensureErr := s.iam.EnsureUser(ctx, proof.User, owner.ID.String())
		if ensureErr != nil {
			return miniAppBootstrapResponse{}, ensureErr
		}
		owner.Subject, owner.Organization = account.Subject, account.OrganizationID
	}
	if err = s.miniAppPrincipal(ctx, owner); err != nil {
		return miniAppBootstrapResponse{}, err
	}
	bearer, err := s.miniAppKey.Bearer(s.options.BotID, attempt.ID, nonce)
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	active, err := s.store.ActivateMiniAppSession(ctx, db.MiniAppActivation{ID: attempt.ID, BotID: s.options.BotID,
		NonceHash: claim.NonceHash, Generation: claim.Generation, BearerHash: sha256.Sum256([]byte(bearer)),
		Subject: owner.Subject, Organization: owner.Organization})
	if err != nil {
		return miniAppBootstrapResponse{}, err
	}
	view, err := s.miniAppView(ctx, active)

	return miniAppBootstrapResponse{miniAppSessionResponse: view, SessionToken: bearer}, err
}

func (s *Server) miniAppPrincipal(ctx context.Context, owner db.User) error {
	_, err := s.miniAppVerifiedPrincipal(ctx, owner)
	return err
}

func (s *Server) miniAppVerifiedPrincipal(ctx context.Context, owner db.User) (identity.Principal, error) {
	if owner.Revoked || owner.BotID != s.options.BotID || owner.Subject == "" ||
		owner.Organization != s.options.OrganizationID {
		s.iam.Invalidate(owner.Subject)
		return identity.Principal{}, identity.ErrDenied
	}
	credential, err := s.iam.UserToken(ctx, owner.Subject)
	if err != nil {
		return identity.Principal{}, err
	}
	principal, err := s.iam.Verify(ctx, credential.Token)
	if err != nil {
		return identity.Principal{}, err
	}
	if principal.Subject != owner.Subject || principal.OrganizationID != owner.Organization ||
		principal.Actor != s.options.ActorID {
		return identity.Principal{}, identity.ErrDenied
	}
	return principal, nil
}

func (s *Server) miniAppView(ctx context.Context, attempt db.MiniAppAttempt) (miniAppSessionResponse, error) {
	profile, err := s.store.GetProfile(ctx, db.ProfileScope{BotID: s.options.BotID, UserID: attempt.User.ID,
		Subject: attempt.User.Subject, Organization: s.options.OrganizationID})
	if err != nil {
		return miniAppSessionResponse{}, err
	}
	return miniAppSessionResponse{
		ExpiresAt:   attempt.ExpiresAt,
		UserID:      profile.UserID,
		FullName:    profile.FullName,
		BotLanguage: profile.BotLanguage,
	}, nil
}

func miniAppBearer(r *http.Request) ([sha256.Size]byte, error) {
	scheme, value, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || scheme != miniAppAuthScheme {
		return [sha256.Size]byte{}, identity.ErrDenied
	}
	if _, err := miniapp.DecodeNonce(value); err != nil {
		return [sha256.Size]byte{}, identity.ErrDenied
	}
	return sha256.Sum256([]byte(value)), nil
}

func (s *Server) miniAppMe(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	view, err := s.miniAppView(ctx, attempt)
	if err == nil {
		_, err = s.currentMiniApp(ctx, r)
	}
	if err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(view)
}

func (s *Server) currentMiniApp(ctx context.Context, r *http.Request) (db.MiniAppAttempt, error) {
	bearer, err := miniAppBearer(r)
	if err != nil {
		return db.MiniAppAttempt{}, err
	}
	return s.store.MiniAppSession(ctx, s.options.BotID, bearer, s.miniAppKey.Generation())
}

func (s *Server) authenticatedMiniApp(ctx context.Context, r *http.Request) (db.MiniAppAttempt, error) {
	attempt, err := s.currentMiniApp(ctx, r)
	if err == nil {
		err = s.miniAppPrincipal(ctx, attempt.User)
	}
	if err != nil {
		return db.MiniAppAttempt{}, err
	}
	// IAM may have taken time; reread current binding/session before the owner read.
	return s.currentMiniApp(ctx, r)
}

func (s *Server) miniAppLogout(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	bearer, err := miniAppBearer(r)
	if err == nil {
		err = s.store.LogoutMiniAppSession(ctx, s.options.BotID, bearer, s.miniAppKey.Generation())
	}
	if err != nil {
		miniAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func miniAppError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, miniapp.ErrInvalid),
		errors.Is(err, identity.ErrDenied),
		errors.Is(err, db.ErrMiniAppDenied),
		errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusUnauthorized, "session unavailable; reopen the Mini App")
	case errors.Is(err, db.ErrConflict), errors.Is(err, identity.ErrConflict):
		miniAppHTTPError(w, http.StatusConflict, "authentication attempt or identity conflicts")
	default:
		w.Header().Set("Retry-After", "1")
		miniAppHTTPError(
			w,
			http.StatusServiceUnavailable,
			"authentication temporarily unavailable; retry the same attempt",
		)
	}
}

func miniAppHTTPError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
