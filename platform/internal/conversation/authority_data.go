package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func decodeReadAuthorities(data json.RawMessage) ([]readsource.Authority, error) {
	var result []readsource.Authority
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if !readsource.Valid(result) {
		return nil, &core.ProblemError{Status: http.StatusInternalServerError, Code: invalidHistory}
	}
	return result, nil
}

func (s *authoritySnapshot) loadEventAuthorities(
	rows []dbgen.CoreConversationReadAuthority,
	extra []readsource.Authority,
) ([]readsource.Authority, error) {
	all := append([]readsource.Authority{}, extra...)
	for _, row := range rows {
		authorities, err := decodeReadAuthorities(row.Authorities)
		if err != nil {
			return nil, err
		}
		s.byEvent[row.EventID] = authorities
		all = append(all, authorities...)
	}
	return all, nil
}

func (s *authoritySnapshot) loadSummaryAuthorities(ctx context.Context, actor string) error {
	data, err := dbgen.New(s.tx).ReadSummaryAuthorities(ctx, actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	s.summary, err = decodeReadAuthorities(data)
	return err
}
