package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

const (
	telegramPrelinkPath       = "/internal/identity/telegram-prelink"
	telegramPrelinkBodyLimit  = 24 << 10
	telegramPrelinkTimeout    = 8 * time.Second
	telegramPrelinkKeyBytes   = 32
	telegramPrelinkFieldLimit = 200
)

// TelegramPrelinkOptions enable only the private provider backchannel.
// The ingress must authenticate TLS before forwarding over the private network.
type TelegramPrelinkOptions struct {
	Issuer, JWKSURL, Audience, ProviderID, ServiceBearer string
	HTTP                                                 *http.Client
}

type telegramPrelinkResponse struct {
	AssertionSHA256 string    `json:"assertion_sha256"`
	OperationID     uuid.UUID `json:"operation_id"`
	State           string    `json:"state"`
	Issuer          string    `json:"issuer"`
	AssertionJTI    string    `json:"assertion_jti"`
	BotID           int64     `json:"bot_id"`
	TelegramUserID  int64     `json:"telegram_user_id"`
	ExternalSubject string    `json:"external_subject"`
	UserID          uuid.UUID `json:"user_id"`
	Subject         string    `json:"subject"`
	Organization    string    `json:"organization"`
	ProviderID      string    `json:"provider_id"`
	ExpiresAt       time.Time `json:"expires_at"`
}

func (s *Server) registerTelegramPrelink() error {
	o := s.options.TelegramPrelink
	if o.Issuer == "" && o.JWKSURL == "" && o.Audience == "" && o.ProviderID == "" && o.ServiceBearer == "" {
		return nil
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(o.ServiceBearer)
	validKey := err == nil && len(secret) == telegramPrelinkKeyBytes &&
		base64.RawURLEncoding.EncodeToString(secret) == o.ServiceBearer
	validProvider := o.ProviderID != "" && len(o.ProviderID) <= telegramPrelinkFieldLimit &&
		utf8.ValidString(o.ProviderID) &&
		!strings.ContainsFunc(o.ProviderID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	dedicatedAudience := s.options.APIAudience != "" && o.Audience != s.options.APIAudience &&
		o.Audience != s.options.LoomAudience
	dedicatedKey := o.ServiceBearer != s.options.WebhookSecret && o.ServiceBearer != s.options.MiniAppBotToken
	if !validKey || !validProvider || !dedicatedAudience || !dedicatedKey {
		return errors.New("dedicated prelink service key, audience and provider required")
	}
	bot, org, issuer := s.iam.DeploymentScope()
	if bot != s.options.BotID || org != s.options.OrganizationID || issuer != s.options.Issuer {
		return errors.New("prelink requires matching API and IAM deployment scope")
	}
	verifier, err := identity.NewTelegramPrelinkVerifier(identity.TelegramPrelinkOptions{
		Issuer: o.Issuer, JWKSURL: o.JWKSURL, Audience: o.Audience, BotID: s.options.BotID,
	}, o.HTTP)
	if err != nil {
		return err
	}
	s.prelinkVerifier = verifier
	s.prelinkBearerHash = sha256.Sum256([]byte("Bearer " + o.ServiceBearer))
	s.internal.HandleFunc("POST "+telegramPrelinkPath, s.telegramPrelink)
	return nil
}

func (s *Server) telegramPrelink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	header := r.Header.Values("Authorization")
	digest := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if len(header) != 1 || subtle.ConstantTimeCompare(digest[:], s.prelinkBearerHash[:]) != 1 {
		http.Error(w, "invalid service authorization", http.StatusUnauthorized)
		return
	}
	if len(r.Header.Values("Origin")) != 0 || r.Header.Get("Sec-Fetch-Site") != "" {
		http.Error(w, "backend request required", http.StatusForbidden)
		return
	}
	if !s.accepting.Load() {
		http.Error(w, "application is restarting", http.StatusServiceUnavailable)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != contentTypeJSON {
		http.Error(w, "JSON request required", http.StatusUnsupportedMediaType)
		return
	}
	deadline := time.Now().Add(telegramPrelinkTimeout)
	controller := http.NewResponseController(w)
	if controller.SetReadDeadline(deadline) != nil || controller.SetWriteDeadline(deadline.Add(time.Second)) != nil {
		http.Error(w, "bounded prelink IO unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	assertion, err := decodeTelegramPrelink(http.MaxBytesReader(w, r.Body, telegramPrelinkBodyLimit))
	if err != nil {
		http.Error(w, "invalid prelink request", http.StatusBadRequest)
		return
	}
	response, err := s.completeTelegramPrelink(ctx, assertion)
	if err != nil {
		telegramPrelinkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeTelegramPrelink(reader io.Reader) (string, error) {
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", identity.ErrDenied
	}
	token, err = decoder.Token()
	if err != nil || token != "assertion" {
		return "", identity.ErrDenied
	}
	var assertion string
	if decoder.Decode(&assertion) != nil || assertion == "" {
		return "", identity.ErrDenied
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return "", identity.ErrDenied
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return "", identity.ErrDenied
	}
	return assertion, nil
}

func (s *Server) completeTelegramPrelink(ctx context.Context, assertion string) (telegramPrelinkResponse, error) {
	proof, err := s.prelinkVerifier.Verify(ctx, assertion, time.Now())
	if err != nil {
		return telegramPrelinkResponse{}, err
	}
	ctx, cancel := context.WithDeadline(ctx, proof.ExpiresAt)
	defer cancel()
	profile, err := json.Marshal(proof.User)
	if err != nil {
		return telegramPrelinkResponse{}, err
	}
	claim := db.TelegramPrelinkClaim{
		BotID: s.options.BotID, TelegramUserID: proof.User.ID, Organization: s.options.OrganizationID,
		ProviderIssuer: proof.Issuer, ProviderID: s.options.TelegramPrelink.ProviderID,
		AssertionJTI: proof.JTI, ExternalSubject: proof.ExternalSubject, Profile: profile,
		AssertionHash: proof.Digest, ExpiresAt: proof.ExpiresAt,
	}
	attempt, err := s.store.ClaimTelegramPrelink(ctx, claim)
	if err != nil {
		return telegramPrelinkResponse{}, err
	}
	if !proof.ExpiresAt.After(time.Now()) {
		return telegramPrelinkResponse{}, identity.ErrDenied
	}
	// An established binding selects the exact IAM account. Never reselect by email.
	account := identity.Account{Subject: attempt.User.Subject, OrganizationID: s.options.OrganizationID,
		SyntheticEmail: fmt.Sprintf("tg+%d+%d@telegram.invalid", s.options.BotID, proof.User.ID)}
	if account.Subject == "" {
		account, err = s.iam.EnsureUser(ctx, proof.User, attempt.User.ID.String())
		if err != nil {
			return telegramPrelinkResponse{}, err
		}
	}
	if !proof.ExpiresAt.After(time.Now()) {
		return telegramPrelinkResponse{}, identity.ErrDenied
	}
	if err = s.iam.EnsureTelegramIDPLink(
		ctx,
		account,
		proof.User.ID,
		claim.ProviderID,
		proof.ExternalSubject,
	); err != nil {
		return telegramPrelinkResponse{}, err
	}
	completed, err := s.store.CompleteTelegramPrelink(ctx, attempt.ID, claim, account.Subject)
	if err != nil {
		return telegramPrelinkResponse{}, err
	}
	return telegramPrelinkResponse{
		OperationID:     completed.ID,
		AssertionSHA256: hex.EncodeToString(proof.Digest[:]),
		State:           completed.State,
		Issuer:          proof.Issuer,
		AssertionJTI:    proof.JTI,
		BotID:           claim.BotID,
		TelegramUserID:  claim.TelegramUserID,
		ExternalSubject: claim.ExternalSubject,
		UserID:          completed.User.ID,
		Subject:         completed.Subject,
		Organization:    claim.Organization,
		ProviderID:      claim.ProviderID,
		ExpiresAt:       completed.ExpiresAt,
	}, nil
}

func telegramPrelinkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrConflict), errors.Is(err, db.ErrConflict):
		http.Error(w, "identity binding conflict", http.StatusConflict)
	case errors.Is(err, identity.ErrDenied), errors.Is(err, db.ErrTelegramPrelinkDenied):
		http.Error(w, "prelink denied", http.StatusForbidden)
	default:
		http.Error(w, "prelink unavailable", http.StatusServiceUnavailable)
	}
}
