package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/identity"
)

const externalAgentTokenPath = "/internal/agent/external-agent/token"
const externalAgentTokenBodyLimit = 64 << 10

func (s *Server) registerExternalAgentTokenService() {
	if s.options.ExternalAgentTokenSecret == "" {
		return
	}
	s.externalAgentPaths = externalAgentReadPaths(s.manifestModules())
	if s.options.ExternalAgentWrites {
		s.externalWritePaths = externalAgentWritePaths(s.manifestModules())
		s.internal.HandleFunc("POST "+externalAgentApprovalPath, s.receiveExternalApproval)
	}
	s.internal.HandleFunc("POST "+externalAgentTokenPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !externalAgentSecretMatches(s.options.ExternalAgentTokenSecret, bearer) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.externalAgentToken(w, r)
	})
}

func (s *Server) externalAgentToken(w http.ResponseWriter, r *http.Request) {
	body := json.NewDecoder(http.MaxBytesReader(w, r.Body, externalAgentTokenBodyLimit))
	body.DisallowUnknownFields()
	var input struct {
		SubjectToken    string `json:"subject_token"`
		ProtocolVersion int    `json:"protocol_version"`
	}
	if err := body.Decode(&input); err != nil || input.ProtocolVersion != 1 || input.SubjectToken == "" ||
		!errors.Is(body.Decode(new(any)), io.EOF) {
		http.Error(w, "invalid token request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	subject, err := s.iam.ExternalAgentSubject(ctx, input.SubjectToken)
	if err != nil {
		externalAgentTokenError(w, err)
		return
	}
	user, err := s.store.GetUserBySubject(ctx, s.options.BotID, s.options.OrganizationID, subject.Subject)
	if err != nil {
		externalAgentTokenError(w, err)
		return
	}
	if user.Revoked || user.Subject != subject.Subject || user.Organization != subject.OrganizationID {
		externalAgentTokenError(w, identity.ErrDenied)
		return
	}
	credential, err := s.iam.IssueExternalAgentToken(ctx, subject)
	if err != nil {
		externalAgentTokenError(w, err)
		return
	}
	expires := int64(time.Until(credential.ExpiresAt) / time.Second)
	if expires <= 0 {
		externalAgentTokenError(w, identity.ErrDenied)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}{credential.Token, expires})
}

func externalAgentTokenError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrDenied) || errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "external agent identity denied", http.StatusForbidden)
		return
	}
	http.Error(w, "token service unavailable", http.StatusServiceUnavailable)
}
