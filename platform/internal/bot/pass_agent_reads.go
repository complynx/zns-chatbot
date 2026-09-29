package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

const registrationReadBytes = 16 * 1024

func (b *Bot) performRegistrationRead(
	ctx context.Context,
	owner string,
	id int64,
	p agent.RegistrationProposal,
	input *agent.Input,
) error {
	if input.Registration == nil || input.Registration.Remaining <= 0 {
		return errors.New("registration read budget exhausted")
	}
	if !registrationCursorKnown(input.Registration, p) {
		return errors.New("registration cursor lacks evidence")
	}
	if (p.View == agent.RegistrationAdminTarget || p.View == agent.RegistrationTakeoverTarget) &&
		!registrationAdminTargetGrounded(*input, p) {
		return errors.New("administrator target lacks evidence")
	}
	if p.View == agent.RegistrationTakeoverTarget && !takeoverEventGrounded(*input, p.Event) {
		return errors.New("takeover event lacks evidence")
	}
	index, err := b.reserveRegistrationRead(ctx, owner, id, p)
	if err != nil {
		return err
	}
	result, err := b.fetchRegistrationRead(ctx, owner, p)
	if err != nil {
		if passMenuFailure(err) != nil {
			return err
		}
		result = agent.RegistrationReadResult{Request: p, Error: "unavailable"}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(encoded) > registrationReadBytes {
		result = agent.RegistrationReadResult{Request: p, Error: "result_budget", Omitted: true}
	}
	_, err = b.DB.Exec(ctx, `UPDATE bot.interactions SET content=jsonb_set(content,ARRAY[$4],$5::jsonb)
	WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, registrationReadsKind, strconv.Itoa(index), result)
	if err != nil {
		return err
	}
	input.Registration.Reads, err = b.authorizedRegistrationReads(ctx, owner, id)
	if err != nil {
		return err
	}
	input.Registration.Remaining = agent.MaxRegistrationReads - len(input.Registration.Reads)
	return boundRegistrationContext(input.Registration)
}

func registrationCursorKnown(context *agent.RegistrationContext, p agent.RegistrationProposal) bool {
	if p.Cursor == "" {
		return true
	}
	if p.View == passMenuEvents && p.Cursor == context.EventCursor {
		return true
	}
	for _, read := range context.Reads {
		if read.Error == "" && read.Request.Event == p.Event && read.Request.View == p.View && read.Next == p.Cursor {
			return true
		}
	}
	return false
}

func (b *Bot) reserveRegistrationRead(
	ctx context.Context,
	owner string,
	id int64,
	p agent.RegistrationProposal,
) (int, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		registrationReadsKind+":"+owner+":"+strconv.FormatInt(id, 10),
	); err != nil {
		return 0, err
	}
	reads := []agent.RegistrationReadResult{}
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, registrationReadsKind).
		Scan(&reads)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	for _, read := range reads {
		if read.Request == p {
			return 0, errors.New("registration read already available")
		}
	}
	if len(reads) >= agent.MaxRegistrationReads {
		return 0, errors.New("registration read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, agent.RegistrationReadResult{Request: p, Error: "read_pending"})
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
	ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, id, registrationReadsKind, reads)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func (b *Bot) fetchRegistrationRead(
	ctx context.Context,
	owner string,
	p agent.RegistrationProposal,
) (agent.RegistrationReadResult, error) {
	result := agent.RegistrationReadResult{Request: p}
	if p.View == agent.RegistrationTakeoverTarget {
		return b.fetchTakeoverTarget(ctx, owner, p, result)
	}
	events, err := b.API.PassEvents(ctx, owner)
	if err != nil {
		return result, err
	}
	if p.View == passMenuEvents {
		result.Events, result.Next, err = registrationEvents(events, p.Cursor)
		return result, err
	}
	found := false
	for _, event := range events {
		if event.ID == p.Event {
			found = true
			break
		}
	}
	if !found {
		result.Error = "unknown_event"
		return result, nil
	}
	booking, err := b.API.PassBooking(ctx, owner, p.Event)
	if err != nil {
		return result, err
	}
	result.Booking = &booking
	return b.fetchRegistrationDetails(ctx, owner, p, result)
}

func (b *Bot) fetchRegistrationDetails(
	ctx context.Context,
	owner string,
	p agent.RegistrationProposal,
	result agent.RegistrationReadResult,
) (agent.RegistrationReadResult, error) {
	var err error
	switch p.View {
	case agent.RegistrationTakeoverTarget:
		return b.fetchTakeoverTarget(ctx, owner, p, result)
	case agent.RegistrationAdminTarget:
		telegramID, parseErr := strconv.ParseInt(p.Target, 10, 64)
		if parseErr != nil {
			return result, parseErr
		}
		target, readErr := b.API.PassAdminTarget(ctx, owner, p.Event, telegramID)
		target.Name = passMenuLabel(target.Name)
		result.AdminTarget = &target
		return result, readErr
	case registrationPayment:
		payment, readErr := b.API.PassPayment(ctx, owner, p.Event, owner)
		result.Payment = &payment
		return result, readErr
	case registrationPaymentQueue:
		page, readErr := b.API.PassPaymentQueue(ctx, owner, p.Event, p.Cursor)
		result.PaymentQueue, result.Next = page.Items, page.Next
		return result, readErr
	case passMenuQueue:
		page, readErr := b.API.PassQueue(ctx, owner, p.Event, p.Cursor)
		result.Queue, result.Next = page.Bookings, page.Next
		return result, readErr
	case passMenuInvitations:
		page, readErr := b.API.PassInvitations(ctx, owner, p.Event, p.Cursor)
		result.Invitations, result.Next = page.Invitations, page.Next
		for index := range result.Invitations {
			result.Invitations[index].From.Name = passMenuLabel(result.Invitations[index].From.Name)
		}
		return result, readErr
	default:
		result.PaymentAdmins, err = b.API.PassPaymentAdmins(ctx, owner, p.Event)
		if len(result.PaymentAdmins) > registrationEventLimit {
			result.PaymentAdmins = result.PaymentAdmins[:registrationEventLimit]
			result.Omitted = true
		}
		for index := range result.PaymentAdmins {
			result.PaymentAdmins[index].Name = passMenuLabel(result.PaymentAdmins[index].Name)
		}
		return result, err
	}
}
