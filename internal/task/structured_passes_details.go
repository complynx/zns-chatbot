package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// Bind every descendant picker to the original owner version and field receipts.
func (s *Service) bindPassPrefill(
	ctx context.Context, t db.Task, c checkpoint, i intent, scope db.ProfileScope, entry *passEntry,
) error {
	if len(entry.Input.Details) == 0 || entry.Prefill != nil {
		return nil
	}
	if i.PassCallback != "" {
		// A picker created before version binding cannot establish the original expectation.
		return db.ErrConflict
	}
	version := c.PassDetailsVersion
	if version == nil {
		status, err := s.store.RegistrationDetailsStatus(ctx, scope)
		if err != nil {
			return err
		}
		version = &status.Version
	}
	entry.Prefill = &passPrefill{Origin: t.ID, Version: *version}
	return nil
}

func (s *Service) storePassInputDetails(
	ctx context.Context, t *db.Task, c *checkpoint, i intent, scope db.ProfileScope, input *passInput,
) error {
	if len(input.Details) == 0 {
		return nil
	}
	protector := s.options.RegistrationDetailsProtector
	raw, err := protector.Open(s.passInputOwner(i.User.ID), input.Details)
	if err != nil {
		return err
	}
	var mutations []registrationdetails.Mutation
	if err = json.Unmarshal([]byte(raw), &mutations); err != nil {
		return registrationdetails.ErrProtection
	}
	source := c.PassEntry.Prefill
	for n, m := range mutations {
		m.OperationID = uuid.NewSHA1(source.Origin, []byte(m.Field))
		mutations[n], err = m.Normalize()
		if err != nil {
			return registrations.ErrInvalid
		}
	}
	if i.PassCallback == "" && c.PassDetailsVersion == nil {
		c.PassDetailsVersion = &source.Version
		if err = s.save(ctx, t, *c, nil); err != nil {
			return s.wait()
		}
	}
	version := source.Version
	for _, m := range mutations {
		m.ExpectedVersion = version
		status, mutationErr := s.store.MutateRegistrationDetails(ctx, scope, m, protector)
		if mutationErr != nil {
			return mutationErr
		}
		version = status.Version
	}
	input.Details = nil
	c.PassEntry.Prefill = nil
	return nil
}

func (s *Service) completePassEntry(
	ctx context.Context, t *db.Task, c checkpoint, i intent, scope db.ProfileScope, payment db.PaymentScope,
	ru bool, screen *passScreen, event events.Event, intro string,
) error {
	input := screen.entry.Input
	selection := *screen.entry.Event
	selection.DetailsOperation = t.ID
	selection.IssueInvite = input.Mode == passModePair
	selection.ClaimInviteCode = input.Code
	operation := passOperation(i, t.ID)
	registration, err := s.store.RegisterPresented(ctx, payment.EventScope, scope.Subject, operation,
		db.RegistrationChange{Kind: registerAction, EventID: event.ID, UserID: scope.UserID, Role: input.Role}, selection)
	if errors.Is(err, registrationdetails.ErrRequired) {
		return s.passDetailsScreen(ctx, t, c, i, scope, ru, screen, intro)
	}
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	intro = ""
	if selection.IssueInvite {
		invite, issueErr := s.store.IssuePassInvite(ctx, payment, event.ID, operation, uuid.Nil)
		return s.passInviteResult(ctx, t, c, i, ru, screen, event, invite, false, issueErr)
	}
	if selection.ClaimInviteCode != "" {
		invite, claimErr := s.store.ReservedPassInvite(ctx, payment, event.ID, input.Code, input.Role)
		if claimErr != nil {
			return s.passInviteResult(ctx, t, c, i, ru, screen, event, invite, false, claimErr)
		}
		if invite.PairID != nil {
			intro = passText(ru, passInvitePaired)
		}
	}
	// A queue worker or an administrator may have assigned the pass after registration.
	registration, err = s.store.Registration(ctx, payment.EventScope, registration.EventID, scope.UserID)
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	status := passCurrentStatus(ru, event, registration, "entry.saved_waiting")
	text, err := s.passQueueSummary(ctx, payment, ru, event.ID, registration.State, passTextParts(intro, status))
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	return s.finishPassScreen(ctx, t, c, i, ru, screen, text)
}

func (s *Service) passDetailsScreen(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.ProfileScope,
	ru bool,
	screen *passScreen,
	intro string,
) error {
	entry := screen.entry.Event
	request, err := s.store.BeginDetailsRequest(ctx, scope, t.ID, db.DetailsRequestStart{
		EventID: entry.EventID, ExpectedEventVersion: entry.EventVersion,
	})
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	request, err = s.store.DetailsRequest(ctx, scope, request.Request.ID)
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	if request.State != detailsRequestPending && request.State != "completed" {
		return s.passEntryError(ctx, t, c, i, ru, db.ErrConflict)
	}
	fields := make([]string, 0, len(request.MissingFields))
	for _, field := range request.MissingFields {
		fields = append(fields, passText(ru, "entry."+string(field)))
	}
	text := passText(ru, "entry.details_ready")
	if len(fields) > 0 {
		text = passText(ru, "entry.details", strings.Join(fields, ", "))
		if s.options.RegistrationDetailsEnabled && s.options.MiniAppURL != "" {
			if err = s.addPassDetailsForm(screen, ru, request.Request.ID); err != nil {
				return err
			}
		} else {
			text = passTextParts(text, passText(ru, "entry.details_unavailable"))
		}
	}
	screen.add(passText(ru, "entry.details_continue"), passChoice{Action: passActionComplete})
	screen.addCancelInvite(ru, screen.entry.Input.Code)
	return s.finishPassScreen(ctx, t, c, i, ru, screen, passTextParts(intro, text))
}

func (s *Service) addPassDetailsForm(screen *passScreen, ru bool, request uuid.UUID) error {
	form, err := detailsFormURL(s.options.MiniAppURL, request)
	if err != nil {
		return err
	}
	screen.rows = append(screen.rows, []telegram.InlineKeyboardButton{{
		Text: passText(ru, "entry.details_form"), WebApp: &telegram.WebAppInfo{URL: form},
	}})
	return nil
}
