// Package browserauth binds Telegram consent to one browser request.
package browserauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

const (
	requestCookie = "zns_browser_request"
	sessionCookie = "zns_browser_session"

	requestTTL      = 5 * time.Minute
	sessionTTL      = 24 * time.Hour
	randomBytes     = 16
	stateAuthorized = "authorized"
	stateApproved   = "approved"
	stateCancelled  = "cancelled"
	sameOriginSite  = "same-origin"
)

var ErrIdentity = errors.New("browser identity unavailable")

type Recipient struct {
	TelegramID int64  `json:"telegram_id"`
	Language   string `json:"language"`
}

type Authenticate func(context.Context, int64) (context.Context, string, error)

type Service struct {
	authenticate  Authenticate
	LegacyOrigins map[string]bool
	DB            *pgxpool.Pool
	Signer        identity.Signer
	TG            telegram.Client
	cookiePath    string
	Origin        string
	Lookup        func(context.Context, string) (Recipient, error)
}

// New accepts the already configured public Web App origin, never proxy headers.
func New(db *pgxpool.Pool, signer identity.Signer, tg telegram.Client, publicURL string,
	lookup func(context.Context, string) (Recipient, error), authenticate Authenticate) (*Service, error) {
	u, err := url.Parse(publicURL)
	if lookup == nil || authenticate == nil || err != nil || u.Host == "" || u.User != nil ||
		(u.Scheme != "https" && u.Scheme != "http") ||
		len(signer.Key) < identity.MinKeyBytes {
		return nil, errors.New("invalid browser authentication configuration")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, errors.New("browser authentication requires HTTPS")
	}
	return &Service{
		cookiePath:   webappurl.Path(publicURL, "/miniapp"),
		DB:           db,
		Signer:       signer,
		TG:           tg,
		Origin:       u.Scheme + "://" + u.Host,
		Lookup:       lookup,
		authenticate: authenticate,
	}, nil
}

func randomID() string {
	value := make([]byte, randomBytes)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func browserHash(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func (s *Service) cookie(w http.ResponseWriter, name, value string, expires time.Time) {
	//nolint:gosec // G124: New allows HTTP only on loopback for local stands; public cookies always use Secure.
	cookie := &http.Cookie{Name: name, Value: value, Path: s.cookiePath, HttpOnly: true,
		Secure: true, SameSite: http.SameSiteStrictMode, Expires: expires}
	// New permits plaintext only on loopback for local functional stands.
	if strings.HasPrefix(s.Origin, "http://") {
		cookie.Secure = false
	}
	http.SetCookie(w, cookie)
}

// SameOrigin protects cookie-authenticated writes. Signed initData keeps its
// existing transport; it does not depend on ambient browser cookies.
func (s *Service) SameOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == s.Origin
}

// ResolveSession rechecks durable consent on every access; the caller must then
// resolve current Telegram identity and rights through its existing API client.
func (s *Service) ResolveSession(r *http.Request) (int64, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return 0, ErrIdentity
	}
	id, err := s.Signer.VerifyBrowserSession(c.Value)
	if err != nil {
		return 0, ErrIdentity
	}
	var sender int64
	err = s.DB.QueryRow(r.Context(), `SELECT telegram_id FROM bot.browser_auth WHERE id=$1 AND origin=$2 AND state='approved' AND session_expires_at>now()`, id, s.Origin).
		Scan(&sender)
	if err != nil {
		return 0, ErrIdentity
	}
	if _, _, err = s.authenticate(r.Context(), sender); err != nil {
		return 0, ErrIdentity
	}
	return sender, nil
}

func (s *Service) create(ctx context.Context, recipient Recipient, secret, origin string) (string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918431054)`); err != nil {
		return "", err
	}
	if _, err = tx.Exec(
		ctx,
		`DELETE FROM bot.browser_auth WHERE expires_at<now()-interval '24 hours' AND (session_expires_at IS NULL OR session_expires_at<now())`,
	); err != nil {
		return "", err
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT count(*)<1000 AND count(*) FILTER(WHERE telegram_id=$1)<3 FROM bot.browser_auth WHERE created_at>now()-interval '5 minutes'`, recipient.TelegramID).
		Scan(&allowed)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errLimited
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE bot.browser_auth SET state='cancelled' WHERE browser_hash=$1 AND state='pending'`,
		browserHash(secret),
	); err != nil {
		return "", err
	}
	id := randomID()
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.browser_auth(id,browser_hash,telegram_id,language,origin) VALUES($1,$2,$3,$4,$5)`,
		id,
		browserHash(secret),
		recipient.TelegramID,
		recipient.Language,
		origin,
	)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}
