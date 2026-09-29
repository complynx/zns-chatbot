package passbooking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type NavigationEvent struct {
	Event

	TitlesExcerpt   bool   `json:"titles_excerpt"`
	DetailAvailable bool   `json:"detail_available"`
	Position        int    `json:"-"`
	Access          string `json:"access"`
}

// NavigationPublic marks entries discoverable independently of a booking.
const NavigationPublic = "public"

// NavigationOwned marks entries whose discovery reveals an owned registration.
const NavigationOwned = "owner"

type eventBoundary struct {
	Version    int       `json:"version"`
	SalesStart time.Time `json:"sales_start"`
	Position   int       `json:"position"`
	ID         string    `json:"id"`
}

const eventBoundaryVersion = 2

func (s Service) EventsPage(ctx context.Context, actor, raw string) (core.ReadPage[NavigationEvent], error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return core.ReadPage[NavigationEvent]{}, err
	}
	cursor, err := core.DecodeReadCursor(raw, actor, "passes.events")
	if err != nil {
		return core.ReadPage[NavigationEvent]{}, err
	}
	var boundary eventBoundary
	if cursor.Position != "" {
		data, decodeErr := base64.RawURLEncoding.DecodeString(cursor.Position)
		if decodeErr != nil || json.Unmarshal(data, &boundary) != nil || boundary.Version != eventBoundaryVersion {
			return core.ReadPage[NavigationEvent]{}, core.ReadProblem("read_cursor_invalid")
		}
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT e.id,jsonb_build_object('en',left(COALESCE(e.titles->>'en',''),$5),'ru',left(COALESCE(e.titles->>'ru',''),$5)),e.finishes_at,e.passport_required,
 (SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),e.display_order,jsonb_build_object('en',left(COALESCE(e.short_titles->>'en',''),$5),'ru',left(COALESCE(e.short_titles->>'ru',''),$5)),left(e.country_emoji,$5),e.open_ended,
 CASE WHEN e.finishes_at>statement_timestamp() THEN 'public' ELSE 'owner' END
 FROM core.pass_events e WHERE (e.finishes_at>statement_timestamp() OR EXISTS(SELECT 1 FROM core.pass_bookings b WHERE b.event_id=e.id AND b.owner=$7)) AND ($1='' OR (COALESCE((SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),'9999-12-31 23:59:59.999999+00'::timestamptz),e.display_order,e.id)>($3,$2,$4))
 ORDER BY COALESCE((SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),'9999-12-31 23:59:59.999999+00'::timestamptz),e.display_order,e.id LIMIT $6`,
		cursor.Position,
		boundary.Position,
		boundary.SalesStart,
		boundary.ID,
		core.ReadExcerptRunes,
		core.ReadPageItems+1,
		actor,
	)
	if err != nil {
		return core.ReadPage[NavigationEvent]{}, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (NavigationEvent, error) {
		item := NavigationEvent{TitlesExcerpt: true, DetailAvailable: true}
		scanErr := row.Scan(
			&item.ID,
			&item.Titles,
			&item.FinishesAt,
			&item.PassportRequired,
			&item.SalesStart,
			&item.Position,
			&item.ShortTitles, &item.CountryEmoji, &item.OpenEnded,
			&item.Access,
		)
		return item, scanErr
	})
	if err != nil {
		return core.ReadPage[NavigationEvent]{}, err
	}
	return core.NavigationPage(items, cursor, func(item NavigationEvent) string {
		start := eventSaleStart(item.Event)
		data, _ := json.Marshal(
			eventBoundary{Version: eventBoundaryVersion, Position: item.Position, ID: item.ID, SalesStart: start},
		)
		return base64.RawURLEncoding.EncodeToString(data)
	})
}

func (s Service) EventDetail(ctx context.Context, actor, eventID, raw string) (core.ReadChunk, error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return core.ReadChunk{}, err
	}
	cursor, err := core.DecodeReadCursor(raw, actor, "passes.event:"+eventID)
	if err != nil {
		return core.ReadChunk{}, err
	}
	var event Event
	var tooLarge bool
	err = s.DB.QueryRow(ctx, `SELECT e.id,CASE WHEN (octet_length(e.titles::text)+octet_length(e.short_titles::text)+octet_length(e.country_emoji))<=$2 THEN e.titles ELSE '{}'::jsonb END,e.finishes_at,e.passport_required,
 (SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),(octet_length(e.titles::text)+octet_length(e.short_titles::text)+octet_length(e.country_emoji))>$2,CASE WHEN (octet_length(e.titles::text)+octet_length(e.short_titles::text)+octet_length(e.country_emoji))<=$2 THEN e.short_titles ELSE '{}'::jsonb END,left(e.country_emoji,$2),e.open_ended FROM core.pass_events e WHERE e.id=$1`, eventID, core.ReadResourceBytes).
		Scan(&event.ID, &event.Titles, &event.FinishesAt, &event.PassportRequired, &event.SalesStart, &tooLarge, &event.ShortTitles, &event.CountryEmoji, &event.OpenEnded)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ReadChunk{}, &core.ProblemError{Status: http.StatusNotFound, Code: "pass_event_unknown"}
	}
	if err != nil {
		return core.ReadChunk{}, err
	}
	if tooLarge {
		return core.ReadChunk{}, core.ReadProblem("read_result_limit")
	}
	return core.JSONReadChunk(event, cursor)
}
