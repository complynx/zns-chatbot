package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func (s *Server) registerPassAdmin() {
	const base = "/app/pass-admin/events/{event}/"
	s.public.HandleFunc("GET "+base+"registrations", s.passAdmin)
	s.public.HandleFunc("GET "+base+"export.xlsx", s.passAdmin)
	s.public.HandleFunc("GET "+base+"identity.zip", s.passIdentityExport)
	s.public.HandleFunc("POST "+base+"return", s.passAdmin)
	s.public.HandleFunc("POST "+base+"refund", s.passAdmin)
}

func (s *Server) passAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	event, err := miniAppPairID(r.PathValue("event"))
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	scope, session, err := s.miniAppPairAssignmentSession(ctx, r)
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	if r.Method == http.MethodPost {
		var input struct {
			db.PassAdminChange

			Confirmed       bool   `json:"confirmed"`
			ConfirmedAmount *int64 `json:"confirmed_minor"`
			RefundAmount    *int64 `json:"refund_minor"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) ||
			!input.Confirmed || input.ConfirmedAmount == nil || input.RefundAmount == nil {
			miniAppPairAssignmentError(w, registrations.ErrInvalid)
			return
		}
		input.PassAdminChange.ConfirmedMinor = *input.ConfirmedAmount
		input.PassAdminChange.RefundMinor = *input.RefundAmount
		receipt, e := s.store.ChangePassReturn(
			ctx,
			scope,
			session,
			event,
			strings.HasSuffix(r.URL.Path, "/refund"),
			input.PassAdminChange,
		)
		s.miniAppPairAssignmentResult(ctx, w, r, scope, session.UserID, receipt, e)
		return
	}
	export := strings.HasSuffix(r.URL.Path, ".xlsx")
	list, err := s.store.PassAdminRows(ctx, scope, session, event, export)
	if s.options.RegistrationDetails == nil {
		list.IdentityExportAvailable = false
	}
	if err != nil || !export {
		s.miniAppPairAssignmentResult(ctx, w, r, scope, session.UserID, list, err)
		return
	}
	workbook, err := passAdminWorkbook(list)
	if err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	if err = s.store.CheckPassAdminExport(ctx, scope, session, event); err != nil {
		miniAppPairAssignmentError(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="passes-`+event.String()+`.xlsx"`)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(workbook) // #nosec G705 -- authorized XLSX attachment with escaped cells, nosniff and sandbox CSP.
}
