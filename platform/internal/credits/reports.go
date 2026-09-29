package credits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type AttemptReport struct {
	ID                string    `json:"id"`
	Operation         string    `json:"operation"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	State             string    `json:"state"`
	PeriodStart       time.Time `json:"period_start"`
	CreatedAt         time.Time `json:"created_at"`
	CostNanoUSD       *int64    `json:"cost_nano_usd"`
	ReservedNanoUSD   *int64    `json:"reserved_nano_usd"`
	CostBasis         string    `json:"cost_basis"`
	ReconciledNanoUSD *int64    `json:"reconciled_nano_usd"`
}

func (s Service) History(ctx context.Context, actor, payer, rawCursor string) (core.ReadPage[AttemptReport], error) {
	if actor == "" || payer == "" || len(actor) > 256 || len(payer) > 256 {
		return core.ReadPage[AttemptReport]{}, ErrInvalid
	}
	if actor != payer {
		if err := requireAdmin(ctx, s.DB, actor); err != nil {
			return core.ReadPage[AttemptReport]{}, err
		}
	}
	hash := sha256.Sum256([]byte("credits.history\x00" + payer))
	actorHash := sha256.Sum256([]byte(actor))
	cursor, err := core.DecodeReadCursor(rawCursor, hex.EncodeToString(actorHash[:]), hex.EncodeToString(hash[:]))
	if err != nil {
		return core.ReadPage[AttemptReport]{}, err
	}
	position := struct {
		Created time.Time `json:"created"`
		ID      string    `json:"id"`
	}{}
	if cursor.Position != "" && json.Unmarshal([]byte(cursor.Position), &position) != nil {
		return core.ReadPage[AttemptReport]{}, core.ReadProblem("read_cursor_invalid")
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT a.id::text,a.operation,a.provider,a.model,a.state,a.period_start,a.created_at,a.cost_nano_usd,a.reserved_nano_usd,a.cost_basis,r.cost_nano_usd
 FROM credits.attempts a LEFT JOIN credits.reconciliations r ON r.attempt_id=a.id WHERE a.payer=$1 AND ($2='' OR (a.created_at,a.id::text)<($3,$2)) ORDER BY a.created_at DESC,a.id::text DESC LIMIT $4`,
		payer,
		position.ID,
		position.Created,
		core.ReadPageItems+1,
	)
	if err != nil {
		return core.ReadPage[AttemptReport]{}, err
	}
	defer rows.Close()
	items := []AttemptReport{}
	for rows.Next() {
		var item AttemptReport
		if err = rows.Scan(
			&item.ID,
			&item.Operation,
			&item.Provider,
			&item.Model,
			&item.State,
			&item.PeriodStart,
			&item.CreatedAt,
			&item.CostNanoUSD,
			&item.ReservedNanoUSD,
			&item.CostBasis,
			&item.ReconciledNanoUSD,
		); err != nil {
			return core.ReadPage[AttemptReport]{}, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return core.ReadPage[AttemptReport]{}, err
	}
	return core.NavigationPage(items, cursor, func(item AttemptReport) string {
		position.Created = item.CreatedAt
		position.ID = item.ID
		raw, _ := json.Marshal(position)
		return string(raw)
	})
}
