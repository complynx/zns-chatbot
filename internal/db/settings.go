package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/shuttle/internal/db/sqlc"
)

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
	if d.AuthCacheTTLSeconds < 0 || d.AuthCacheTTLSeconds > 300 {
		return VersionedSettings{}, fmt.Errorf("invalid auth cache TTL")
	}
	if (d.DefaultLanguage != "en" && d.DefaultLanguage != "ru") || d.MaxParallelTasks < 1 || d.MaxParallelTasks > 128 ||
		d.QuestionTimeoutSeconds < 1 ||
		d.QuestionTimeoutSeconds > 604800 ||
		actor == "" {
		return VersionedSettings{}, fmt.Errorf("invalid settings")
	}
	document, e := json.Marshal(d)
	if e != nil {
		return VersionedSettings{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return VersionedSettings{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
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
	if e = q.AuditSettings(
		ctx,
		sqlc.AuditSettingsParams{Actor: actor, Version: r.Version, OldDocument: old.Document, NewDocument: document},
	); e != nil {
		return VersionedSettings{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return VersionedSettings{}, e
	}
	return VersionedSettings{r.Version, d}, nil
}
