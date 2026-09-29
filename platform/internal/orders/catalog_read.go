package orders

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// CatalogRead pages the complete administrator-managed catalog. The API checks
// current identity on every request; the cursor binds actor, event and digest.
// Agent output stays at 4000 runes; host hydration uses bounded HTTP chunks.
func (s Service) CatalogRead(ctx context.Context, actor, event, raw string, transport bool) (core.ReadChunk, error) {
	cursor, err := core.DecodeReadCursor(raw, actor, "orders.catalog:"+event)
	if err != nil {
		return core.ReadChunk{}, err
	}
	value, err := s.Event(ctx, event)
	if err != nil {
		return core.ReadChunk{}, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return core.ReadChunk{}, err
	}
	if transport {
		return core.JSONReadTransportChunk(data, cursor)
	}
	return core.JSONReadDataChunk(data, cursor)
}
