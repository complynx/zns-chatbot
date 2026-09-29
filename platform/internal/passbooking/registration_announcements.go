package passbooking

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

type RegistrationAnnouncement struct {
	ID       int64  `json:"id"`
	Channel  string `json:"channel"`
	ThreadID *int64 `json:"thread_id"`
	Locale   string `json:"locale"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Attempts int64  `json:"attempts"`
}

type AnnouncementCompletion struct {
	ID      int64            `json:"id"`
	Attempt int64            `json:"attempt"`
	Outcome delivery.Outcome `json:"outcome"`
}

// ClaimRegistrationAnnouncement is retained for callers without the shared dispatcher.
func (s Service) ClaimRegistrationAnnouncement(ctx context.Context) (RegistrationAnnouncement, bool, error) {
	if err := s.RecoverRegistrationAnnouncements(ctx); err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	entries, err := delivery.Candidates(ctx, tx, s.Delivery.BotID, announcementCandidateLimit)
	_ = tx.Rollback(ctx)
	if err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	for _, entry := range entries {
		if entry.Reference.Owner != delivery.Announcement {
			continue
		}
		id, parseErr := strconv.ParseInt(entry.Reference.Key, 10, 64)
		if parseErr != nil {
			return RegistrationAnnouncement{}, false, parseErr
		}
		item, found, prepareErr := s.PrepareRegistrationAnnouncement(ctx, id)
		if prepareErr != nil || found {
			return item, found, prepareErr
		}
	}
	return RegistrationAnnouncement{}, false, nil
}
func (s Service) BeginRegistrationAnnouncement(
	ctx context.Context,
	attempt delivery.Attempt,
) (delivery.Admission, error) {
	if err := s.Delivery.Validate(); err != nil {
		return delivery.Admission{}, err
	}
	if attempt.ID <= 0 || attempt.Generation <= 0 {
		return delivery.Admission{}, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return delivery.Admission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	row, err := s.lockAnnouncementAdmission(ctx, q, attempt)
	if err != nil {
		return delivery.Admission{}, err
	}
	if row.State != operationPending || !row.LeaseLive {
		return delivery.Admission{}, conflict("pass_announcement_stale")
	}
	if !row.Current {
		outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "announcement_superseded"}
		deadline := time.Now()
		scheduleErr := delivery.Project(
			ctx,
			tx,
			s.Delivery.BotID,
			announcementReference(attempt.ID),
			delivery.Cancelled,
			deadline,
		)
		if scheduleErr != nil {
			return delivery.Admission{}, scheduleErr
		}
		if err = s.finishAnnouncement(ctx, q, attempt, outcome, deadline); err != nil {
			return delivery.Admission{}, err
		}
		return delivery.Admission{Reason: outcome.Reason}, tx.Commit(ctx)
	}
	gate, err := delivery.Begin(ctx, tx, s.Delivery, announcementReference(attempt.ID))
	if err != nil {
		return gate, err
	}
	if !gate.Ready {
		err = s.finishAnnouncement(
			ctx,
			q,
			attempt,
			delivery.Outcome{Kind: delivery.Deferred, Reason: gate.Reason},
			gate.NotBefore,
		)
	} else {
		var count int64
		count, err = q.BeginAnnouncementSend(
			ctx,
			dbgen.BeginAnnouncementSendParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
		)
		if err == nil && count != 1 {
			err = conflict("pass_announcement_stale")
		}
	}
	if err != nil {
		return delivery.Admission{}, err
	}
	return gate, tx.Commit(ctx)
}

func (s Service) CompleteRegistrationAnnouncement(ctx context.Context, input AnnouncementCompletion) error {
	if input.ID <= 0 || input.Attempt <= 0 || !input.Outcome.Valid() {
		return invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	row, err := q.LockAnnouncementAttempt(
		ctx,
		dbgen.LockAnnouncementAttemptParams{ID: input.ID, BotID: s.Delivery.BotID, Attempt: input.Attempt},
	)
	if err != nil {
		return announcementAttemptError(err)
	}
	if row.State == operationPending &&
		(input.Outcome.Kind == delivery.Succeeded || input.Outcome.Kind == delivery.Uncertain) {
		return conflict("pass_announcement_stale")
	}
	outcome, deadline, err := delivery.Finish(ctx, tx, s.Delivery, announcementReference(input.ID), input.Outcome)
	if err != nil {
		return err
	}
	if err = s.finishAnnouncement(
		ctx,
		q,
		delivery.Attempt{ID: input.ID, Generation: input.Attempt},
		outcome,
		deadline,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) finishAnnouncement(
	ctx context.Context,
	q *dbgen.Queries,
	attempt delivery.Attempt,
	outcome delivery.Outcome,
	deadline time.Time,
) error {
	var failures int64
	if outcome.Kind == delivery.Rejected {
		failures = 1
	}
	count, err := q.FinishAnnouncement(
		ctx,
		dbgen.FinishAnnouncementParams{
			ID:      attempt.ID,
			BotID:   s.Delivery.BotID,
			Attempt: attempt.Generation,
			State: string(
				outcome.Kind,
			),
			MessageID:        outcome.MessageID,
			Failure:          outcome.Reason,
			AvailableAt:      pgtype.Timestamptz{Time: deadline, Valid: true},
			FailureIncrement: failures,
		},
	)
	if err == nil && count != 1 {
		return conflict("pass_announcement_stale")
	}
	return err
}

func announcementAttemptError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("pass_announcement_stale")
	}
	return err
}

// Lock current domain authority before the outbox attempt, matching mutation lock order.
func (s Service) lockAnnouncementAdmission(
	ctx context.Context,
	q *dbgen.Queries,
	attempt delivery.Attempt,
) (dbgen.LockAnnouncementAttemptRow, error) {
	source, err := q.AnnouncementAttemptSource(
		ctx,
		dbgen.AnnouncementAttemptSourceParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, announcementAttemptError(err)
	}
	if _, err = q.LockAnnouncementEvent(ctx, source.EventID); err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, err
	}
	if _, err = q.LockAnnouncementBooking(
		ctx,
		dbgen.LockAnnouncementBookingParams(source),
	); err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, err
	}
	row, err := q.LockAnnouncementAttempt(
		ctx,
		dbgen.LockAnnouncementAttemptParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, announcementAttemptError(err)
	}
	return row, nil
}
