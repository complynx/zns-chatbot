package massage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type NavigationSlot struct {
	SlotChoice

	NameExcerpt     string `json:"name_excerpt"`
	IconExcerpt     string `json:"icon_excerpt"`
	DetailAvailable bool   `json:"detail_available"`
}

func readSlotSnapshot(
	ctx context.Context,
	q queryer,
	event, party string,
	navigation bool,
) ([]Provider, []Reservation, error) {
	if !navigation {
		return snapshot(ctx, q, event, party)
	}
	staff, err := readProviders(ctx, q, event, true)
	if err != nil {
		return nil, nil, err
	}
	bookings, err := reservations(ctx, q, event, party)
	return staff, bookings, err
}

func (s Service) SlotsPage(
	ctx context.Context,
	actor, event, party string,
	length int,
	raw string,
) (core.ReadPage[NavigationSlot], error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return core.ReadPage[NavigationSlot]{}, err
	}
	scope, _ := json.Marshal([]any{"massage.slots", event, party, length})
	cursor, err := core.DecodeReadCursor(raw, actor, string(scope))
	if err != nil {
		return core.ReadPage[NavigationSlot]{}, err
	}
	available, err := s.slots(ctx, actor, event, party, length, true)
	if err != nil {
		return core.ReadPage[NavigationSlot]{}, err
	}
	byID := make(map[string]PublicProvider, len(available.Providers))
	for _, provider := range available.Providers {
		byID[provider.Owner] = provider
	}
	items := make([]NavigationSlot, 0, len(available.Slots))
	for _, slot := range available.Slots {
		provider := byID[slot.Specialist]
		items = append(
			items,
			NavigationSlot{
				SlotChoice:      slot,
				NameExcerpt:     provider.Name,
				IconExcerpt:     provider.Icon,
				DetailAvailable: true,
			},
		)
	}
	data, err := json.Marshal(items)
	if err != nil {
		return core.ReadPage[NavigationSlot]{}, err
	}
	digest := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(digest[:])
	if cursor.Digest != "" && cursor.Digest != fingerprint {
		return core.ReadPage[NavigationSlot]{}, core.ReadProblem("read_stale")
	}
	offset := 0
	if cursor.Position != "" {
		offset, err = strconv.Atoi(cursor.Position)
		if err != nil || offset < 0 || offset > len(items) {
			return core.ReadPage[NavigationSlot]{}, core.ReadProblem("read_cursor_invalid")
		}
	}
	cursor.Digest = fingerprint
	return core.NavigationPage(
		items[offset:],
		cursor,
		func(_ NavigationSlot) string { offset++; return strconv.Itoa(offset) },
	)
}

func (s Service) ProviderDetail(ctx context.Context, actor, event, provider, raw string) (core.ReadChunk, error) {
	if err := authenticated(ctx, s.DB, actor); err != nil {
		return core.ReadChunk{}, err
	}
	scope, _ := json.Marshal([]string{"massage.provider", event, provider})
	cursor, err := core.DecodeReadCursor(raw, actor, string(scope))
	if err != nil {
		return core.ReadChunk{}, err
	}
	var value PublicProvider
	var about []byte
	var tooLarge bool
	err = s.DB.QueryRow(ctx, `SELECT s.owner,u.telegram_id,
 CASE WHEN octet_length(s.name)+octet_length(s.icon)+octet_length(s.about::text)<=$3 THEN s.name ELSE '' END,
 CASE WHEN octet_length(s.name)+octet_length(s.icon)+octet_length(s.about::text)<=$3 THEN s.icon ELSE '' END,
 CASE WHEN octet_length(s.name)+octet_length(s.icon)+octet_length(s.about::text)<=$3 THEN s.about ELSE '{}'::jsonb END,
 s.min_length,s.max_length,octet_length(s.name)+octet_length(s.icon)+octet_length(s.about::text)>$3
 FROM core.massage_specialists s JOIN core.users u ON u.id=s.owner WHERE s.event_id=$1 AND s.owner=$2`, event, provider, core.ReadResourceBytes).
		Scan(&value.Owner, &value.ID, &value.Name, &value.Icon, &about, &value.MinLength, &value.MaxLength, &tooLarge)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ReadChunk{}, &core.ProblemError{Status: http.StatusNotFound, Code: "not_found"}
	}
	if err != nil {
		return core.ReadChunk{}, core.DatabaseOperationError(err)
	}
	if about != nil {
		if err = json.Unmarshal(about, &value.About); err != nil {
			return core.ReadChunk{}, err
		}
	}
	if tooLarge {
		return core.ReadChunk{}, core.ReadProblem("read_result_limit")
	}
	return core.JSONReadChunk(value, cursor)
}
