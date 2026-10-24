package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

type profileNameSyncArgs struct {
	UserID  uuid.UUID `json:"user_id"`
	Version int64     `json:"version"`
}

func (profileNameSyncArgs) Kind() string { return "zinusya_profile_name_sync" }

type profileNameSyncWorker struct {
	river.WorkerDefaults[profileNameSyncArgs]

	service *Service
}

func (w *profileNameSyncWorker) Work(ctx context.Context, job *river.Job[profileNameSyncArgs]) error {
	started := time.Now()
	err := w.service.syncProfileName(ctx, job.Args.UserID)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) repairProfileNames(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	bot, organization, _ := s.iam.DeploymentScope()
	rows, err := s.store.ReadyProfileNames(ctx, bot, organization)
	if err == nil {
		for _, row := range rows {
			_, err = s.queue.Insert(ctx, profileNameSyncArgs{UserID: row.UserID, Version: row.Version},
				&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}})
			if err != nil {
				break
			}
		}
	}
	if err != nil && ctx.Err() == nil {
		s.record(ctx, "profile_name_sync", metricError)
		s.logger.WarnContext(ctx, "profile name scheduling unavailable", "class", "profile_name_queue")
	}
}

func (s *Service) syncProfileName(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, s.options.AttemptTimeout)
	defer cancel()
	u, err := s.store.GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	bot, organization, _ := s.iam.DeploymentScope()
	err = s.store.DispatchProfileName(ctx, db.ProfileScope{
		BotID: bot, Organization: organization, Subject: u.Subject, UserID: userID,
	}, func(name db.ProfileNameSync) error {
		return s.iam.SyncProfileName(ctx, identity.Account{
			Subject: u.Subject, OrganizationID: organization,
			SyntheticEmail: fmt.Sprintf("tg+%d+%d@telegram.invalid", bot, u.TelegramUserID),
		}, u.TelegramUserID, identity.ProfileName{FirstName: name.FirstName, LastName: name.LastName})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if retry, ok := errors.AsType[*db.ProfileNameRetryError](err); ok {
		return river.JobSnooze(retry.After)
	}
	return err
}
