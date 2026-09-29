package browserauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

const (
	legacyRequestCookie = "zns_legacy_request"
	legacySessionCookie = "zns_legacy_session"
	legacyPollInterval  = 250 * time.Millisecond
)

// ConfigureLegacyOrigins opts into exact HTTPS origins separated by spaces.
// Empty disables third-party cookies and CORS, retaining first-party /auth.
func (s *Service) ConfigureLegacyOrigins(raw string) error {
	var err error
	s.LegacyOrigins, err = identity.BrowserOrigins(raw)
	if err != nil {
		return err
	}
	if len(s.LegacyOrigins) > 0 && !strings.HasPrefix(s.Origin, "https://") {
		return errors.New("cross-origin browser authentication requires a public HTTPS URL")
	}
	return nil
}

func (s *Service) legacyCookie(w http.ResponseWriter, name, value, origin string, expires time.Time) {
	//nolint:gosec // G124: HTTP is loopback-only; cross-site cookies require an exact HTTPS allowlist and origin-bound consent.
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
	}
	if s.LegacyOrigins[origin] {
		cookie.SameSite = http.SameSiteNoneMode
	}
	if strings.HasPrefix(s.Origin, "http://") {
		cookie.Secure = false
	}
	http.SetCookie(w, cookie)
}

// LegacyHandler preserves historical GET check/username and long-poll JSON.
// Validating Origin protects this legacy client which has no CSRF header.
func (s *Service) LegacyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" && r.Header.Get("Sec-Fetch-Site") == sameOriginSite {
			origin = s.Origin
		}
		if origin != s.Origin && !s.LegacyOrigins[origin] {
			failure(w, http.StatusForbidden, "origin_rejected")
			return
		}
		if s.LegacyOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			failure(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		if r.URL.Query().Has("check") {
			s.legacyCheck(w, r)
			return
		}
		s.legacyStart(w, r, origin)
	})
}

func (s *Service) legacyCheck(w http.ResponseWriter, r *http.Request) {
	_, err := s.ResolveLegacySession(r)
	if err != nil {
		reply(w, http.StatusUnauthorized, map[string]string{resultField: "unauthorized"})
		return
	}
	reply(w, http.StatusOK, map[string]string{resultField: stateAuthorized})
}

// ResolveLegacySession is for allowed legacy routes. Their adapter must check
// Origin and resolve current Telegram identity and rights, as Mini App does.
func (s *Service) ResolveLegacySession(r *http.Request) (int64, error) {
	c, err := r.Cookie(legacySessionCookie)
	if err != nil {
		return 0, ErrIdentity
	}
	id, err := s.Signer.VerifyBrowserSession(c.Value)
	if err != nil {
		return 0, ErrIdentity
	}
	var sender int64
	origin := r.Header.Get("Origin")
	if origin == "" && r.Header.Get("Sec-Fetch-Site") == sameOriginSite {
		origin = s.Origin
	}
	if origin != s.Origin && !s.LegacyOrigins[origin] {
		return 0, ErrIdentity
	}
	err = s.DB.QueryRow(r.Context(), `SELECT telegram_id FROM bot.browser_auth WHERE id=$1 AND origin=$2 AND state='approved' AND session_expires_at>now()`, id, origin).
		Scan(&sender)
	if err != nil {
		return 0, ErrIdentity
	}
	if _, _, err = s.authenticate(r.Context(), sender); err != nil {
		return 0, ErrIdentity
	}
	return sender, nil
}

func (s *Service) legacyStart(w http.ResponseWriter, r *http.Request, origin string) {
	username := strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("username")), "@")
	if !usernamePattern.MatchString(username) {
		failure(w, http.StatusBadRequest, "invalid_username")
		return
	}
	recipient, err := s.Lookup(r.Context(), username)
	if err != nil || recipient.TelegramID <= 0 {
		failure(w, http.StatusNotFound, "recipient_unavailable")
		return
	}
	secret := randomID()
	if c, cookieErr := r.Cookie(legacyRequestCookie); cookieErr == nil && len(c.Value) == 32 {
		secret = c.Value
	}
	id, err := s.create(r.Context(), recipient, secret, origin)
	if errors.Is(err, errLimited) {
		failure(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if err != nil {
		failure(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	s.legacyCookie(w, legacyRequestCookie, secret, origin, time.Now().Add(sessionTTL))
	if err = s.sendConsent(r.Context(), recipient, id, origin); err != nil {
		s.cancelLegacy(id)
		failure(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	// The old client waits for headers, not the JSON body. Never flush early.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(requestTTL + time.Second))
	state, expires := s.waitLegacy(r.Context(), id)
	if state == stateApproved {
		s.legacyCookie(w, legacySessionCookie, s.Signer.BrowserSession(id, expires), origin, expires)
		reply(w, http.StatusOK, map[string]string{resultField: stateAuthorized})
		return
	}
	s.cancelLegacy(id)
	reply(w, http.StatusUnauthorized, map[string]string{resultField: state})
}

func (s *Service) cancelLegacy(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.DB.Exec(ctx, `UPDATE bot.browser_auth SET state='cancelled' WHERE id=$1 AND state='pending'`, id)
}

func (s *Service) waitLegacy(ctx context.Context, id string) (string, time.Time) {
	ticker := time.NewTicker(legacyPollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(requestTTL)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return stateCancelled, time.Time{}
		case <-deadline.C:
			return stateCancelled, time.Time{}
		case <-ticker.C:
			var state string
			var expires time.Time
			err := s.DB.QueryRow(ctx, `SELECT CASE WHEN expires_at<=now() THEN 'cancelled' ELSE state END,COALESCE(session_expires_at,expires_at) FROM bot.browser_auth WHERE id=$1`, id).
				Scan(&state, &expires)
			if err != nil {
				return "unauthorized", time.Time{}
			}
			if state != "pending" {
				return state, expires
			}
		}
	}
}
