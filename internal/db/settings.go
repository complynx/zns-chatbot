package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const defaultNameExpectationSeconds = 7200
const defaultMassageMaxBookingsPerWindow = 3

func (s *Store) Settings(ctx context.Context) (VersionedSettings, error) {
	r, e := s.q.GetSettings(ctx)
	if e != nil {
		return VersionedSettings{}, e
	}
	var d Settings
	e = json.Unmarshal(r.Document, &d)
	return VersionedSettings{r.Version, d}, e
}

func (s *Store) CompareAndSwapSettings(
	ctx context.Context,
	version int64,
	d Settings,
	actor string,
) (VersionedSettings, error) {
	if err := validateSettings(d, actor); err != nil {
		return VersionedSettings{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VersionedSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := compareAndSwapSettingsTx(ctx, s.q.WithTx(tx), version, d, actor)
	if err != nil {
		return VersionedSettings{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return VersionedSettings{}, err
	}
	return result, nil
}

func validateSettings(d Settings, actor string) error {
	if _, _, _, err := d.SymbolPolicy(); err != nil {
		return err
	}
	if _, err := d.AvatarBrowserPolicy(); err != nil {
		return err
	}
	if _, _, err := d.MassageReminderLeads(); err != nil {
		return err
	}
	if _, err := d.MiniAppPolicy(); err != nil {
		return err
	}
	if d.AuthCacheTTLSeconds < 0 || d.AuthCacheTTLSeconds > 300 {
		return fmt.Errorf("invalid auth cache TTL")
	}
	if (d.DefaultLanguage != "en" && d.DefaultLanguage != "ru") || d.MaxParallelTasks < 1 || d.MaxParallelTasks > 128 ||
		d.QuestionTimeoutSeconds < 1 ||
		d.QuestionTimeoutSeconds > 604800 ||
		(d.MassageMaxBookingsPerWindow < 0 || d.MassageMaxBookingsPerWindow > 100) ||
		(d.NameExpectationSeconds != 0 && (d.NameExpectationSeconds < 60 || d.NameExpectationSeconds > 86400)) ||
		actor == "" {
		return fmt.Errorf("invalid settings")
	}
	return nil
}

// compareAndSwapSettingsTx writes validated settings and audit. The caller owns
// validation and the transaction, so an agent receipt can share the same commit.
func compareAndSwapSettingsTx(
	ctx context.Context, q *sqlc.Queries, version int64, d Settings, actor string,
) (VersionedSettings, error) {
	document, e := json.Marshal(d)
	if e != nil {
		return VersionedSettings{}, e
	}
	old, e := q.GetSettings(ctx)
	if e != nil {
		return VersionedSettings{}, e
	}
	if old.Version != version {
		return VersionedSettings{}, ErrConflict
	}
	r, e := q.CASSettings(ctx, sqlc.CASSettingsParams{Version: version, Document: document})
	if errors.Is(e, pgx.ErrNoRows) {
		return VersionedSettings{}, ErrConflict
	}
	if e != nil {
		return VersionedSettings{}, e
	}
	var previous Settings
	if e = json.Unmarshal(old.Document, &previous); e != nil {
		return VersionedSettings{}, e
	}
	if e = rescheduleMassageReminders(ctx, q, previous, d); e != nil {
		return VersionedSettings{}, e
	}
	if e = q.AuditSettings(
		ctx,
		sqlc.AuditSettingsParams{Actor: actor, Version: r.Version, OldDocument: old.Document, NewDocument: document},
	); e != nil {
		return VersionedSettings{}, e
	}
	return VersionedSettings{r.Version, d}, nil
}
