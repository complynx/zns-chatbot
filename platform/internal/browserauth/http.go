package browserauth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	errLimited      = errors.New("browser authentication rate limited")
)

const resultField = "result"
const maxBodyBytes = 1024

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func failure(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"code": code})
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	authRoute(mux, http.MethodPost, "/miniapp/auth/start", s.start)
	authRoute(mux, http.MethodGet, "/miniapp/auth/status", s.status)
	authRoute(mux, http.MethodPost, "/miniapp/auth/cancel", s.cancel)
	authRoute(mux, http.MethodPost, "/miniapp/auth/logout", s.logout)
	authRoute(mux, http.MethodGet, "/miniapp/auth/check", func(w http.ResponseWriter, r *http.Request) {
		_, err := s.ResolveSession(r)
		if err != nil {
			failure(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		reply(w, http.StatusOK, map[string]string{resultField: stateAuthorized})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		failure(w, http.StatusNotFound, "not_found")
	})
	return GuardRouting(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sameSiteRead := r.Method == http.MethodGet && r.Header.Get("Origin") == "" &&
			r.Header.Get("Sec-Fetch-Site") == sameOriginSite
		if r.Header.Get("X-Browser-Auth") != "1" ||
			(!s.SameOrigin(r) && !sameSiteRead) {
			failure(w, http.StatusForbidden, "origin_rejected")
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func authRoute(mux *http.ServeMux, method, path string, handler http.HandlerFunc) {
	mux.HandleFunc(method+" "+path, handler)
	allowed := method
	if method == http.MethodGet {
		allowed += ", HEAD"
	}
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allowed)
		failure(w, http.StatusMethodNotAllowed, "method_not_allowed")
	})
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		failure(w, http.StatusBadRequest, "invalid_username")
		return
	}
	input.Username = strings.TrimPrefix(strings.TrimSpace(input.Username), "@")
	if !usernamePattern.MatchString(input.Username) {
		failure(w, http.StatusBadRequest, "invalid_username")
		return
	}
	recipient, err := s.Lookup(r.Context(), input.Username)
	if err != nil || recipient.TelegramID <= 0 {
		failure(w, http.StatusBadRequest, "recipient_unavailable")
		return
	}
	secret := randomID()
	if cookie, cookieErr := r.Cookie(requestCookie); cookieErr == nil && len(cookie.Value) == 32 {
		secret = cookie.Value
	}
	id, err := s.create(r.Context(), recipient, secret, s.Origin)
	if errors.Is(err, errLimited) {
		failure(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if err != nil {
		failure(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	s.cookie(w, requestCookie, secret, time.Now().Add(sessionTTL))
	if err = s.sendConsent(r.Context(), recipient, id, s.Origin); err != nil {
		_, _ = s.DB.Exec(r.Context(), `UPDATE bot.browser_auth SET state='cancelled' WHERE id=$1`, id)
		failure(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	reply(w, http.StatusOK, map[string]string{"request": id, resultField: "pending"})
}

func (s *Service) boundRequest(r *http.Request) (string, []byte, error) {
	c, err := r.Cookie(requestCookie)
	id := r.URL.Query().Get("request")
	if err != nil || len(c.Value) != 32 || len(id) != 32 {
		return "", nil, ErrIdentity
	}
	return id, browserHash(c.Value), nil
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	id, hash, err := s.boundRequest(r)
	if err != nil {
		failure(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var state string
	var expires time.Time
	err = s.DB.QueryRow(r.Context(), `SELECT CASE WHEN expires_at<=now() THEN 'expired' ELSE state END,COALESCE(session_expires_at,expires_at) FROM bot.browser_auth WHERE id=$1 AND browser_hash=$2`, id, hash).
		Scan(&state, &expires)
	if err != nil {
		failure(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if state == stateApproved {
		s.cookie(w, sessionCookie, s.Signer.BrowserSession(id, expires), expires)
	}
	reply(w, http.StatusOK, map[string]string{resultField: state})
}

func (s *Service) cancel(w http.ResponseWriter, r *http.Request) {
	id, hash, err := s.boundRequest(r)
	if err != nil {
		failure(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	result, err := s.DB.Exec(
		r.Context(),
		`UPDATE bot.browser_auth SET state='cancelled' WHERE id=$1 AND browser_hash=$2`,
		id,
		hash,
	)
	if err != nil {
		failure(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if result.RowsAffected() == 0 {
		failure(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	reply(w, http.StatusOK, map[string]string{resultField: stateCancelled})
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if id, verifyErr := s.Signer.VerifyBrowserSession(c.Value); verifyErr == nil {
			if _, err = s.DB.Exec(
				r.Context(),
				`UPDATE bot.browser_auth SET state='cancelled' WHERE id=$1`,
				id,
			); err != nil {
				failure(w, http.StatusServiceUnavailable, "unavailable")
				return
			}
		}
	}
	s.cookie(w, sessionCookie, "", time.Unix(1, 0))
	reply(w, http.StatusOK, map[string]string{resultField: stateCancelled})
}
