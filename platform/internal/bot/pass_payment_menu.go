package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const registrationPayment = "payment"
const registrationPaymentQueue = "payment_queue"
const registrationProofAccept = "proof_accept"
const registrationProofReject = "proof_reject"

type registrationProofReference struct {
	Event   string `json:"event"`
	Owner   string `json:"owner"`
	Attempt string `json:"attempt"`
	Version int64  `json:"version"`
}

func (r *passMenuRenderer) paymentNavigation(ctx context.Context, booking passbooking.Booking) error {
	if booking.State == registrationAssigned || booking.State == statePaid {
		r.navigate(i18n.RegistrationPayment, registrationPayment)
	}
	_, err := r.bot.API.PassPaymentQueue(ctx, r.owner, r.state.Event, "")
	if err == nil {
		r.takeoverLink(0, "")
		r.navigate(i18n.RegistrationPaymentQueue, registrationPaymentQueue)
	}
	return passMenuFailure(err)
}

func (r *passMenuRenderer) payment(ctx context.Context) error {
	booking, err := r.bot.API.PassBooking(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	if booking.State == registrationAssigned {
		quote, quoteErr := r.bot.API.PassPaymentQuote(ctx, r.owner, r.state.Event)
		if quoteErr != nil {
			return quoteErr
		}
		total, formatErr := i18n.FormatNumber(r.language, strconv.FormatInt(quote.Total, 10))
		if formatErr != nil {
			return formatErr
		}
		text, translateErr := i18n.Translate(
			r.language,
			i18n.RegistrationPaymentTotal,
			map[string]string{orderTotalKey: total},
		)
		if translateErr != nil {
			return translateErr
		}
		r.lines = append(r.lines, text)
		r.lines = append(r.lines, r.text(i18n.RegistrationPaymentHint))
	}
	payment, err := r.bot.API.PassPayment(ctx, r.owner, r.state.Event, r.owner)
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	if problem, ok := errors.AsType[*core.ProblemError](
		err,
	); ok &&
		(problem.Status == http.StatusNotFound || problem.Status == http.StatusForbidden) {
		return nil
	}
	if err != nil {
		return err
	}
	r.lines = append(r.lines, r.paymentStatus(payment))
	r.paymentFile(r.owner, payment, "")
	return nil
}

func (r *passMenuRenderer) paymentStatus(payment passbooking.Payment) string {
	id := i18n.RegistrationPaymentPending
	switch payment.Decision {
	case "accepted":
		id = i18n.RegistrationPaymentAccepted
	case registrationRejected:
		id = i18n.RegistrationPaymentRejected
	}
	return r.text(id)
}

func (r *passMenuRenderer) paymentFile(owner string, payment passbooking.Payment, label string) {
	if payment.ProofUnavailable {
		r.lines = append(r.lines, r.text(i18n.RegistrationPaymentFileUnavailable))
	}
	if payment.ProofID == "" {
		return
	}
	ref := registrationProofReference{
		Event:   payment.Event,
		Owner:   owner,
		Attempt: payment.Attempt,
		Version: payment.Version,
	}
	r.choices = append(r.choices, passMenuChoice{
		label:  r.text(i18n.RegistrationPaymentFile) + label,
		action: passMenuAction{PaymentFile: &ref},
	})
}

const registrationAssigned = "assigned"
const registrationRejected = "rejected"
const statePaid = "paid"

func (r *passMenuRenderer) paymentQueue(ctx context.Context) error {
	view, err := (interaction.RegistrationMenuReader{Domain: r.bot.API}).
		PaymentQueue(ctx, r.owner, r.state.Event, r.state.After, nil)
	if err != nil {
		return err
	}
	first, last := r.page(len(view.Entries), view.Page.Next)
	for _, entry := range view.Entries[first:last] {
		item := entry.Review
		r.takeoverLink(item.TelegramID, " · "+strconv.FormatInt(item.TelegramID, 10))
		// The queue authorizes this contact; no unrelated profile is fetched.
		label := strconv.FormatInt(item.TelegramID, 10)
		r.contact(passbooking.Contact{Owner: item.Owner, TelegramID: item.TelegramID, Name: label})
		r.lines = append(r.lines, label+" · "+r.paymentStatus(item.Payment))
		r.paymentFile(item.Owner, item.Payment, " · "+label)
		for _, command := range []passbooking.Command{entry.Accept, entry.Reject} {
			var id i18n.ID
			switch command.Name {
			case registrationProofAccept:
				id = i18n.RegistrationPaymentAccept
			case registrationProofReject:
				id = i18n.RegistrationPaymentReject
			}
			r.choices = append(
				r.choices,
				passMenuChoice{label: r.text(id) + " · " + label, action: passMenuAction{Command: &command}},
			)
		}
	}
	return nil
}

func (b *Bot) sendRegistrationProof(ctx context.Context, in incoming, ref registrationProofReference) error {
	_, err := b.queueBotDocument(
		ctx,
		in.owner,
		in.chat,
		botdelivery.Reference{
			Family:       botFamilyPassProof,
			Event:        ref.Event,
			Object:       ref.Owner,
			Version:      ref.Version,
			ProofAttempt: ref.Attempt,
			Continuation: botdelivery.Continuation{Kind: botDocumentKind},
		},
	)
	return err
}
