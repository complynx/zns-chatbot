package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const (
	participantPaymentPath    = "/app/pass-payments/"
	miniAppPassPaymentRecent  = "recent"
	miniAppPassPaymentPurpose = "purpose"
	miniAppPassPaymentNull    = "null"
)

func (s *Server) registerMiniAppPassPayments() {
	for _, path := range []string{"targets",
		miniAppPassPaymentRecent,
		"events/{event}/passes/{pass}",
		"events/{event}/files",
		"events/{event}/evidence",
		"events/{event}/evidence/{evidence}",
		"events/{event}/operations/{operation}"} {
		s.public.HandleFunc("GET "+participantPaymentPath+path, s.miniAppPassPaymentRead)
	}
	s.public.HandleFunc("POST "+participantPaymentPath+"events/{event}/evidence", s.miniAppPassPaymentSubmit)
	s.public.HandleFunc(
		"GET "+participantPaymentPath+"events/{event}/files/{file}/content",
		s.miniAppPassPaymentContent,
	)
}

// miniAppPassPaymentSession never grants authority over another participant.
func (s *Server) miniAppPassPaymentSession(
	ctx context.Context,
	r *http.Request,
) (db.PaymentScope, db.MiniAppPaymentEvidenceSession, error) {
	scope := db.PaymentScope{EventScope: s.eventScope()}
	var session db.MiniAppPaymentEvidenceSession
	attempt, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return scope, session, err
	}
	unchanged := s.iam.InvalidationFence(attempt.User.Subject)
	principal, err := s.miniAppVerifiedPrincipal(ctx, attempt.User)
	if err != nil {
		return scope, session, err
	}
	if !unchanged() || !time.Now().Before(principal.ExpiresAt) || !time.Now().Before(attempt.ExpiresAt) ||
		principal.Subject != attempt.User.Subject ||
		principal.OrganizationID != scope.Organization {
		return scope, session, db.ErrMiniAppDenied
	}
	scope.UserID, scope.Subject = attempt.User.ID, principal.Subject
	session.UserID = attempt.User.ID
	session.Bearer, err = miniAppBearer(r)
	session.Generation = s.miniAppKey.Generation()
	session.AuthorityUnchanged = func() bool {
		return unchanged() && time.Now().Before(principal.ExpiresAt) && time.Now().Before(attempt.ExpiresAt)
	}
	return scope, session, err
}

func (s *Server) miniAppPassPaymentRefresh(
	ctx context.Context,
	r *http.Request,
	scope db.PaymentScope,
	session db.MiniAppPaymentEvidenceSession,
) (db.MiniAppPaymentEvidenceSession, error) {
	if session.AuthorityUnchanged == nil || !session.AuthorityUnchanged() {
		return session, db.ErrMiniAppDenied
	}
	current, fresh, err := s.miniAppPassPaymentSession(ctx, r)
	if err != nil {
		return fresh, err
	}
	if current != scope || fresh.UserID != session.UserID || fresh.Bearer != session.Bearer ||
		fresh.Generation != session.Generation ||
		!session.AuthorityUnchanged() {
		return fresh, db.ErrMiniAppDenied
	}
	return fresh, nil
}

func (s *Server) miniAppPassPaymentResult(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	scope db.PaymentScope,
	session db.MiniAppPaymentEvidenceSession,
	result any,
	err error,
) {
	if err == nil {
		_, err = s.miniAppPassPaymentRefresh(ctx, r, scope, session)
	}
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}

func miniAppPassPaymentID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, payments.ErrInvalid
	}
	return id, nil
}

func miniAppPassPaymentPage(r *http.Request) (uuid.UUID, int32, error) {
	after := uuid.Nil
	if values, ok := r.URL.Query()["after"]; ok {
		if len(values) != 1 {
			return after, 0, payments.ErrInvalid
		}
		var err error
		after, err = miniAppPassPaymentID(values[0])
		if err != nil {
			return after, 0, err
		}
	}
	limit := int32(miniAppPaymentPageSize)
	if values, ok := r.URL.Query()["limit"]; ok {
		if len(values) != 1 {
			return after, 0, payments.ErrInvalid
		}
		n, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || n < 1 || n > miniAppPaymentPageSize {
			return after, 0, payments.ErrInvalid
		}
		limit = int32(n)
	}
	return after, limit, nil
}

func (s *Server) miniAppPassPaymentRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	after, limit, err := miniAppPassPaymentPage(r)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPassPaymentSession(ctx, r)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	result, err := s.miniAppPassPaymentReadResult(ctx, r, scope, session, after, limit)
	s.miniAppPassPaymentResult(ctx, w, r, scope, session, result, err)
}

func (s *Server) miniAppPassPaymentReadResult(
	ctx context.Context,
	r *http.Request,
	scope db.PaymentScope,
	session db.MiniAppPaymentEvidenceSession,
	after uuid.UUID,
	limit int32,
) (any, error) {
	switch r.URL.Path {
	case participantPaymentPath + "targets":
		return s.store.MiniAppPaymentTargets(ctx, scope, session, after, limit)
	case participantPaymentPath + miniAppPassPaymentRecent:
		actions, err := s.store.MiniAppRecentPaymentEvidenceActions(ctx, scope, session)
		return struct {
			Actions []db.NativePaymentEvidenceAction `json:"actions"`
		}{actions}, err
	}
	event, err := miniAppPassPaymentID(r.PathValue("event"))
	if err != nil {
		return nil, err
	}
	switch {
	case r.PathValue("pass") != "":
		pass, parseErr := miniAppPassPaymentID(r.PathValue("pass"))
		if parseErr != nil {
			return nil, parseErr
		}
		return s.store.MiniAppOwnPaymentTarget(ctx, scope, session, event, pass)
	case r.PathValue("evidence") != "":
		evidence, parseErr := miniAppPassPaymentID(r.PathValue("evidence"))
		if parseErr != nil {
			return nil, parseErr
		}
		return s.store.MiniAppOwnPaymentEvidence(ctx, scope, session, event, evidence)
	case r.PathValue("operation") != "":
		operation := r.PathValue("operation")
		if len(operation) > paymentOperationIDLimit {
			return nil, payments.ErrInvalid
		}
		return s.store.MiniAppPaymentSubmissionReceipt(ctx, scope, session, event, operation)
	case r.URL.Path == participantPaymentPath+"events/"+r.PathValue("event")+"/files":
		return s.store.MiniAppPaymentEvidenceFiles(ctx, scope, session, event, after, limit)
	default:
		pass, parseErr := miniAppPassPaymentFilter(r)
		if parseErr != nil {
			return nil, parseErr
		}
		return s.store.MiniAppOwnPaymentEvidenceList(ctx, scope, session, event, pass, after, limit)
	}
}

func miniAppPassPaymentFilter(r *http.Request) (uuid.UUID, error) {
	values, ok := r.URL.Query()["pass_id"]
	if !ok {
		return uuid.Nil, nil
	}
	if len(values) != 1 {
		return uuid.Nil, payments.ErrInvalid
	}
	return miniAppPassPaymentID(values[0])
}

type miniAppPassPaymentInput struct {
	db.MiniAppPaymentEvidenceSubmission

	OperationID string
}

// Decode field-by-field to reject duplicate fields as well as unknown, null and absent claims.
func decodeMiniAppPassPayment(reader io.Reader) (miniAppPassPaymentInput, error) {
	var input miniAppPassPaymentInput
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return input, payments.ErrInvalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return input, payments.ErrInvalid
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return input, payments.ErrInvalid
		}
		seen[key] = true
		var target any
		switch key {
		case avatarProposalOperationField:
			target = &input.OperationID
		case "pass_id":
			target = &input.PassID
		case "file_id":
			target = &input.FileID
		case "amount_minor":
			target = &input.AmountMinor
		case miniAppPassPaymentPurpose:
			target = &input.Purpose
		default:
			return input, payments.ErrInvalid
		}
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil || string(raw) == miniAppPassPaymentNull {
			return input, payments.ErrInvalid
		}
		if err = json.Unmarshal(raw, target); err != nil {
			return input, payments.ErrInvalid
		}
	}
	if _, err = decoder.Token(); err != nil {
		return input, payments.ErrInvalid
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return input, payments.ErrInvalid
	}
	if len(seen) != 5 || input.OperationID == "" || len(input.OperationID) > paymentOperationIDLimit ||
		input.PassID == uuid.Nil ||
		input.FileID == uuid.Nil ||
		input.AmountMinor <= 0 ||
		input.AmountMinor > payments.MaxAmount ||
		(input.Purpose != payments.Initial && input.Purpose != payments.TopUp) {
		return input, payments.ErrInvalid
	}
	return input, nil
}

func (s *Server) miniAppPassPaymentSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := miniAppPassPaymentID(r.PathValue("event"))
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	input, err := decodeMiniAppPassPayment(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPassPaymentSession(ctx, r)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	result, err := s.store.SubmitMiniAppPaymentEvidence(
		ctx,
		scope,
		session,
		input.OperationID,
		event,
		input.MiniAppPaymentEvidenceSubmission,
	)
	s.miniAppPassPaymentResult(ctx, w, r, scope, session, result, err)
}
