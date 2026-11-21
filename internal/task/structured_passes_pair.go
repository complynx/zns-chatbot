package task

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const passDraftLookupTimeout = 2 * time.Second

func (s *Service) currentPassEntryMenu(
	ctx context.Context, t *db.Task, c checkpoint, i intent, scope db.ProfileScope, payment db.PaymentScope,
	ru bool, screen *passScreen, event events.Event, now time.Time,
) (bool, error) {
	claim, err := s.store.PendingPassClaim(ctx, payment, event.ID)
	if err == nil {
		input := &screen.entry.Input
		input.Code, input.Role, input.Mode = claim.Code, claim.Role, ""
		text := passSaleUnavailable(ru, event, now)
		if text == "" {
			text = passText(ru, "entry.invite_claimed", passText(ru, "entry."+claim.Role))
			screen.add(passText(ru, "entry.details_continue"), passChoice{Action: passActionComplete})
		}
		screen.addCancelInvite(ru, claim.Code)
		return true, s.finishPassScreen(ctx, t, c, i, ru, screen, text)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return true, s.identityError(ctx, t, c, i, err)
	}
	registered, err := s.store.Registration(ctx, payment.EventScope, event.ID, payment.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, s.identityError(ctx, t, c, i, err)
	}
	return true, s.passRegistrationMenu(ctx, t, c, i, scope, payment, ru, screen, event, registered, now)
}

func passSaleUnavailable(ru bool, event events.Event, now time.Time) string {
	rules := event.Document.Registration
	if rules == nil || !now.Before(rules.ClosesAt) || !now.Before(event.Document.EndsAt) {
		return passText(ru, "entry.closed")
	}
	if rules.SalesOpensAt != nil && now.Before(*rules.SalesOpensAt) {
		return passText(ru, "entry.opens", event.Document.Name, passLocalTime(*rules.SalesOpensAt, event.Document.Timezone))
	}
	return ""
}

func (p *passScreen) addCancelInvite(ru bool, code string) {
	if code != "" {
		p.add(passText(ru, "entry.revoke_invite"), passChoice{Action: "revoke", Value: code})
	}
}

func (s *Service) passRegistrationMenu(
	ctx context.Context, t *db.Task, c checkpoint, i intent, scope db.ProfileScope, payment db.PaymentScope,
	ru bool, screen *passScreen, event events.Event, registration registrations.Registration, now time.Time,
) error {
	if registration.State == db.PassReturned {
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, "entry.returned"))
	}
	if screen.entry.Input.Role != "" && screen.entry.Input.Role != registration.Role {
		return s.passEntryError(ctx, t, c, i, ru, db.ErrConflict)
	}
	if err := s.storePassInputDetails(ctx, t, &c, i, scope, &screen.entry.Input); err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	if s.options.RegistrationDetailsEnabled && s.options.MiniAppURL != "" {
		request, err := s.store.EnrolledPassDetailsRequest(ctx, payment, event)
		if err != nil {
			return s.passEntryError(ctx, t, c, i, ru, err)
		}
		if request != uuid.Nil {
			if err = s.addPassDetailsForm(screen, ru, request); err != nil {
				return err
			}
		}
	}
	text := passCurrentStatus(ru, event, registration, "entry.waiting")
	text, err := s.passQueueSummary(ctx, payment, ru, event.ID, registration.State, text)
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	pairs, err := s.store.OwnedPairs(ctx, payment, event.ID, uuid.Nil, 1)
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	if len(pairs) > 0 {
		pair := pairs[0].Pair
		screen.add(passText(ru, "entry.leave_pair"), passChoice{Action: "leave_confirm", ID: pair.ID, Version: pair.Version})
		text = passTextParts(text, passText(ru, "entry.pair_current"))
	}
	if passSaleUnavailable(ru, event, now) != "" {
		return s.finishPassScreen(ctx, t, c, i, ru, screen, text)
	}
	invite, err := s.store.ActivePassInvite(ctx, payment, event.ID)
	if err == nil {
		return s.passInviteResult(ctx, t, c, i, ru, screen, event, invite, len(pairs) > 0, nil)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	return s.passExistingPairChoices(ctx, t, c, i, payment, ru, screen, event, registration, pairs, text)
}

func (s *Service) passExistingPairChoices(
	ctx context.Context, t *db.Task, c checkpoint, i intent, payment db.PaymentScope, ru bool, screen *passScreen,
	event events.Event, registration registrations.Registration, pairs []db.OwnedPair, text string,
) error {
	waiting := registration.State == registrations.Waitlisted
	if len(pairs) > 0 {
		if waiting {
			screen.add(passText(ru, "entry.replace_pair"), passChoice{Action: passActionReplace, ID: pairs[0].Pair.ID})
		}
		return s.finishPassScreen(ctx, t, c, i, ru, screen, text)
	}
	if waiting && screen.entry.Input.Mode == passModePair {
		invite, err := s.store.IssuePassInvite(ctx, payment, event.ID, passOperation(i, t.ID), uuid.Nil)
		return s.passInviteResult(ctx, t, c, i, ru, screen, event, invite, false, err)
	}
	if waiting {
		screen.add(passText(ru, "entry.create_pair"), passChoice{Action: "issue"})
		screen.add(passText(ru, "entry.join_pair"), passChoice{Action: passActionJoin})
	}
	return s.finishPassScreen(ctx, t, c, i, ru, screen, text)
}

func passCurrentStatus(ru bool, event events.Event, registration registrations.Registration, waitingKey string) string {
	switch registration.State {
	case db.PassReturned:
		return passText(ru, "entry.returned")
	case registrations.Assigned:
		pass := registration.Pass
		if pass == nil {
			return passText(ru, "entry.saved_assigned")
		}
		zone, err := time.LoadLocation(event.Document.Timezone)
		if err != nil {
			zone = time.UTC
		}
		return passAssignmentText(structuredText(ru, "en", "ru"), event.Document.Name, registration.Role, zone, *pass)
	default:
		if !event.Document.Registration.AutoAssign {
			return passText(ru, "ops.waitlistedState")
		}
		return passText(ru, waitingKey)
	}
}

func (s *Service) passQueueSummary(
	ctx context.Context, scope db.PaymentScope, ru bool, eventID uuid.UUID, state, text string,
) (string, error) {
	if state != registrations.Waitlisted {
		return text, nil
	}
	position, err := s.store.PassQueuePosition(ctx, scope, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return text, nil
	}
	if err != nil {
		return "", err
	}
	if position.Individual > 0 {
		text = passTextParts(text, passText(ru, "entry.individual_position", position.Individual))
	}
	if position.Role > 0 {
		text = passTextParts(text, passText(ru, "entry.role_position", position.Role))
	}
	if position.Pair > 0 {
		text = passTextParts(text, passText(ru, "entry.pair_position", position.Pair))
	}
	return text, nil
}

func (s *Service) passInviteResult(
	ctx context.Context, t *db.Task, c checkpoint, i intent, ru bool, screen *passScreen,
	event events.Event, invite db.PassInvite, replacement bool, err error,
) error {
	switch {
	case passChoiceUnavailable(err):
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, "entry.invite_unavailable"))
	case err != nil:
		return s.identityError(ctx, t, c, i, err)
	case invite.PairID != nil:
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, passInvitePaired))
	case invite.Revoked:
		return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, "entry.invite_revoked"))
	}
	command := passesCommand + " --invite " + invite.Code
	if event.Document.Slug != "" {
		command = passesCommand + " " + event.Document.Slug + " --invite " + invite.Code
	}
	link := s.passDraftLink(ctx, command)
	fallback := ""
	if link == "" {
		link = passesCommand + " " + invite.Code
	} else {
		fallback = passText(ru, "entry.invite_command")
	}
	key := "entry.invite_issued"
	if replacement {
		key = "entry.invite_replacement"
	}
	screen.addCancelInvite(ru, invite.Code)
	return s.finishPassScreen(ctx, t, c, i, ru, screen, passTextParts(passText(ru, key, invite.Code, link), fallback))
}

func (s *Service) passDraftLink(ctx context.Context, command string) string {
	client, ok := s.tg.(interface {
		GetMe(context.Context) (telegram.User, error)
	})
	if !ok {
		return ""
	}
	lookup, cancel := context.WithTimeout(ctx, passDraftLookupTimeout)
	defer cancel()
	bot, err := client.GetMe(lookup)
	if err != nil {
		return ""
	}
	return telegram.PassDraftURL(bot, s.options.BotID, startCommand+" "+command)
}

func (s *Service) passSimpleResult(
	ctx context.Context, t *db.Task, c checkpoint, i intent, ru bool, screen *passScreen, key string, err error,
) error {
	if err != nil {
		return s.passEntryError(ctx, t, c, i, ru, err)
	}
	return s.finishPassScreen(ctx, t, c, i, ru, screen, passText(ru, key))
}
