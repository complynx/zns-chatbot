package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type AudienceUser struct {
	UserID string   `json:"user_id"`
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

// Audience exposes bounded navigation, never SQL or a silent audience cap.
func (s Service) Audience(ctx context.Context, actor, rawCursor string) (core.ReadPage[AudienceUser], error) {
	var result core.ReadPage[AudienceUser]
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	cursor, err := core.DecodeReadCursor(rawCursor, actor, "broadcast.audience")
	if err != nil {
		return result, err
	}
	var after int64
	if cursor.Position != "" {
		after, err = strconv.ParseInt(cursor.Position, 10, 64)
		if err != nil {
			return result, core.ReadProblem("read_cursor_invalid")
		}
	}
	rows, err := tx.Query(
		ctx,
		`SELECT u.telegram_id,p.fields||p.overrides FROM core.users u LEFT JOIN core.admin_broadcast_profiles p ON p.owner=u.id WHERE u.telegram_id>$1 ORDER BY u.telegram_id LIMIT $2`,
		after,
		core.ReadPageItems+1,
	)
	if err != nil {
		return result, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AudienceUser, error) {
		var item AudienceUser
		var id int64
		var fields map[string]any
		scanErr := row.Scan(&id, &fields)
		if scanErr != nil {
			return item, scanErr
		}
		if fields == nil {
			return item, rejected("admin_message_selector_profile_unavailable")
		}
		s.scopeProfile(fields)
		item.UserID = strconv.FormatInt(id, 10)
		name := []rune(broadcastUserName(fields, id))
		item.Name = string(name[:min(len(name), core.ReadExcerptRunes)])
		for field := range fields {
			item.Fields = append(item.Fields, field)
		}
		slices.Sort(item.Fields)
		return item, nil
	})
	if err != nil {
		return result, err
	}
	result, err = core.NavigationPage(items, cursor, func(item AudienceUser) string { return item.UserID })
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s Service) AudienceProfile(ctx context.Context, actor, userID, rawCursor string) (core.ReadChunk, error) {
	var result core.ReadChunk
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || id <= 0 {
		return result, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	cursor, err := core.DecodeReadCursor(rawCursor, actor, "broadcast.profile:"+strconv.FormatInt(id, 10))
	if err != nil {
		return result, err
	}
	var profile json.RawMessage
	err = tx.QueryRow(ctx, `SELECT ((p.fields||p.overrides)-'bot_id')||jsonb_build_object('user_id',u.telegram_id)||CASE WHEN $2::bigint>0 THEN jsonb_build_object('bot_id',$2::bigint) ELSE '{}'::jsonb END FROM core.users u JOIN core.admin_broadcast_profiles p ON p.owner=u.id WHERE u.telegram_id=$1`, id, s.BotID).
		Scan(&profile)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, rejected("admin_message_selector_profile_unavailable")
	}
	if err != nil {
		return result, err
	}
	result, err = core.JSONReadChunk(profile, cursor)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
