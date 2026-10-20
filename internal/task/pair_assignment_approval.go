package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

// Pair consent displays the original actor-owned terms and current identities of
// that pair's participants. Display names are context, never approval or legal identity.
func (s *Service) pairAssignmentApprovalText(
	ctx context.Context,
	t db.Task,
	i intent,
	a loom.Approval,
) (string, error) {
	if a.OperationID == "" || len(a.OperationID) > 128 {
		return "", errors.New("invalid pair approval operation")
	}
	var args struct {
		db.PairAssignment

		ResumeProposalID *uuid.UUID `json:"resume_proposal_id,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(a.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("trailing pair approval arguments")
	}
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return "", err
	}
	if user.BotID != s.options.BotID || user.TelegramUserID != i.User.ID {
		return "", errors.New("pair approval recipient mismatch")
	}
	// IAM work completes before the store takes event/actor locks.
	principal, err := s.taskPrincipal(ctx, user)
	if err != nil {
		return "", err
	}
	view, err := s.store.PairAssignmentProposalByOperation(ctx, s.taskPaymentScope(user, principal), a.OperationID)
	if err != nil {
		return "", err
	}
	p := view.Preparation
	validState := view.State == db.AssignmentProposalPending || view.State == db.AssignmentProposalCompleted
	if !validState || p.Description == "" || p.Arguments != args.PairAssignment ||
		a.ExpiresAt.After(p.ExpiresAt) ||
		view.Leader == nil ||
		view.Follower == nil ||
		view.Leader.UserID == uuid.Nil ||
		view.Follower.UserID == uuid.Nil ||
		view.Leader.TelegramUserID <= 0 ||
		view.Follower.TelegramUserID <= 0 ||
		(args.ResumeProposalID != nil && *args.ResumeProposalID != p.ID) {
		return "", errors.New("pair approval does not match preparation")
	}
	return fullPairAssignmentApproval(view, a.ExpiresAt, s.languageFor(ctx, i))
}

func fullPairAssignmentApproval(
	view db.PairAssignmentProposalView,
	expires time.Time,
	language string,
) (string, error) {
	zoneName := view.Preparation.Timezone
	if zoneName == "" {
		zoneName = "UTC"
	}
	zone, err := time.LoadLocation(zoneName)
	if err != nil {
		return "", err
	}
	if len(view.Preparation.Quotes) != 0 && len(view.Preparation.Quotes) != 2 {
		return "", errors.New("invalid pair approval quote count")
	}
	title, leader, follower, terms := "Assign both passes?", "Leader (current display identity)", "Follower (current display identity)", "Original terms"
	if language == "ru" {
		title, leader, follower, terms = "Назначить оба пасса?", "Лидер (текущее отображаемое имя)", "Фолловер (текущее отображаемое имя)", "Исходные условия"
	}
	var body strings.Builder
	writeParticipant := func(label string, person db.PairAssignmentParticipant) {
		fmt.Fprintf(
			&body,
			"%s: %s (Telegram %d; account %s)\n",
			label,
			strconv.Quote(person.DisplayName),
			person.TelegramUserID,
			person.UserID,
		)
	}
	writeParticipant(leader, *view.Leader)
	writeParticipant(follower, *view.Follower)
	fmt.Fprintf(&body, "\n%s:\n%s", terms, view.Preparation.Description)
	for n, quote := range view.Preparation.Quotes {
		label := leader
		if n == 1 {
			label = follower
		}
		writePairAssignmentDepositTerms(&body, label, quote, zone, language == "ru")
	}
	if language == "ru" {
		fmt.Fprintf(&body, "\nПодтвердить до: %s (%s)\n", expires.In(zone).Format(time.RFC3339), zoneName)
	} else {
		fmt.Fprintf(&body, "\nConfirm before: %s (%s)\n", expires.In(zone).Format(time.RFC3339), zoneName)
	}
	if view.State == db.AssignmentProposalCompleted {
		if view.Result == nil {
			return "", errors.New("completed pair approval lacks original result")
		}
		if language == "ru" {
			title = "Это назначение уже выполнено. Продолжение сверит исходный результат и не назначит пассы повторно."
		} else {
			title = "This assignment was already completed. Continue to reconcile the original result; this does not assign passes again."
		}
	}

	text := body.String()
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return title + "\n\n" + fence + "\n" + text + "\n" + fence, nil
}

// Configured minimum amount/share and final top-up deadline supplement the
// original description, which already contains each price, tier and required deposit.
func writePairAssignmentDepositTerms(
	body *strings.Builder,
	label string,
	quote db.AssignmentQuote,
	zone *time.Location,
	ru bool,
) {
	if quote.Deposit == nil {
		return
	}
	minimum, share, final := "Minimum amount", "Minimum share", "Final top-up deadline"
	if ru {
		minimum, share, final = "Минимальная сумма", "Минимальная доля", "Срок доплаты"
	}
	const basisPointsPerPercent = 100
	d := quote.Deposit
	fmt.Fprintf(
		body,
		"\n%s\n%s: %s\n%s: %d.%02d%%\n%s: %s (%s)\n",
		label,
		minimum,
		orderApprovalMoney(d.MinimumMinor, int(quote.CurrencyScale), quote.Currency),
		share,
		d.MinimumBPS/basisPointsPerPercent,
		d.MinimumBPS%basisPointsPerPercent,
		final,
		d.FinalDueAt.In(zone).Format(time.RFC3339),
		zone.String(),
	)
}
