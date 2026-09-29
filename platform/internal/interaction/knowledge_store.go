package interaction

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func (s Store) loadKnowledgeAssessment(
	ctx context.Context,
	owner string,
	updateID int64,
	kind string,
) (agent.KnowledgeAssessment, bool, error) {
	var value agent.KnowledgeAssessment
	err := s.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, kind).
		Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, false, nil
	}
	return value, err == nil, err
}

// saveKnowledgeAssessment reads the durable winner after insertion. Concurrent
// classifiers and retries must submit that winner, never their losing verdict.
func (s Store) saveKnowledgeAssessment(
	ctx context.Context,
	owner string,
	updateID int64,
	kind string,
	value agent.KnowledgeAssessment,
) (agent.KnowledgeAssessment, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return agent.KnowledgeAssessment{}, err
	}
	_, err = s.DB.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		owner,
		updateID,
		kind,
		raw,
	)
	if err != nil {
		return agent.KnowledgeAssessment{}, err
	}
	stored, found, err := s.loadKnowledgeAssessment(ctx, owner, updateID, kind)
	if err == nil && !found {
		err = pgx.ErrNoRows
	}
	return stored, err
}
