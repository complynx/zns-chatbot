package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
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
	page, err := r.bot.API.PassPaymentQueue(ctx, r.owner, r.state.Event, r.state.After)
	if err != nil {
		return err
	}
	own, err := r.bot.API.PassBooking(ctx, r.owner, r.state.Event)
	if err != nil {
		return err
	}
	first, last := r.page(len(page.Items), page.Next)
	for _, item := range page.Items[first:last] {
		r.takeoverLink(item.TelegramID, " · "+strconv.FormatInt(item.TelegramID, 10))
		// The queue authorizes this contact; no unrelated profile is fetched.
		label := strconv.FormatInt(item.TelegramID, 10)
		r.contact(passbooking.Contact{Owner: item.Owner, TelegramID: item.TelegramID, Name: label})
		r.lines = append(r.lines, label+" · "+r.paymentStatus(item.Payment))
		r.paymentFile(item.Owner, item.Payment, " · "+label)
		for _, accept := range []bool{true, false} {
			name, id := registrationProofReject, i18n.RegistrationPaymentReject
			if accept {
				name, id = registrationProofAccept, i18n.RegistrationPaymentAccept
			}
			command := passbooking.Command{Name: name, Event: r.state.Event, Version: own.Version,
				Target: item.Owner, TargetVersion: item.Payment.Version, PaymentAttempt: item.Payment.Attempt}
			r.choices = append(
				r.choices,
				passMenuChoice{label: r.text(id) + " · " + label, action: passMenuAction{Command: &command}},
			)
		}
	}
	return nil
}

func (b *Bot) sendRegistrationProof(ctx context.Context, in incoming, ref registrationProofReference) error {
	proof, err := b.API.DownloadPassProof(ctx, in.owner, ref.Event, ref.Owner)
	if err != nil {
		return err
	}
	if proof.Version != ref.Version || proof.Attempt != ref.Attempt {
		return &core.ProblemError{Status: http.StatusConflict, Code: "pass_payment_stale"}
	}
	_, err = b.TG.SendDocument(ctx, in.chat, proof.Filename, proof.Body)
	return err
}
