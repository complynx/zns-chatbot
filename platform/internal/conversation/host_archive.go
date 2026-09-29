package conversation

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// DerivedArchive carries trusted host provenance, never model-selected authority.
type DerivedArchive struct {
	SourceKey          string                 `json:"source_key"`
	Text               string                 `json:"text"`
	ReplyToUpdateID    int64                  `json:"reply_to_update_id"`
	Media              bool                   `json:"media"`
	ExpectedGeneration *int64                 `json:"expected_generation"`
	ReadAuthorities    []readsource.Authority `json:"read_authorities"`
}

// ArchiveOriginal accepts live user/manual input. Imported assistant originals
// retain the separate AppendOriginal entrypoint.
func (s Service) ArchiveOriginal(ctx context.Context, actor, key, kind, text string) error {
	if err := (OriginalArchive{SourceKey: key, Kind: kind, Text: text}).Validate(); err != nil {
		return err
	}
	return s.AppendOriginal(ctx, actor, key, kind, text)
}

func (s Service) ArchiveOutcome(ctx context.Context, actor, key, text string) error {
	if err := (OutcomeArchive{SourceKey: key, Text: text}).Validate(); err != nil {
		return err
	}
	return s.AppendTrustedOutcome(ctx, actor, key, text)
}

func (s Service) ArchiveDerived(ctx context.Context, actor string, input DerivedArchive) error {
	if err := input.Validate(); err != nil {
		return err
	}
	var sensitive bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.conversation_events
 WHERE owner=$1 AND source_key=$2 AND text='[sensitive text omitted]')`, actor,
		"tg-user-"+strconv.FormatInt(input.ReplyToUpdateID, 10)).Scan(&sensitive)
	if err != nil {
		return err
	}
	text := input.Text
	if sensitive {
		text = "[response to sensitive request omitted]"
	}
	if input.Media {
		text = "[media response; expiring content omitted]"
	}
	return s.AppendDerived(ctx, actor, input.SourceKey, text, *input.ExpectedGeneration, input.ReadAuthorities)
}
