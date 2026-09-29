package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// PreparedWorkflow retains target authorization and receipt state so application
// orchestration can fence derived sources before applying a new selection.
type PreparedWorkflow struct {
	service Service
	tx      pgx.Tx
	owner   string
	action  Action
	hash    string
	replay  Workflow
	found   bool
}

func (s Service) PrepareWorkflowInTx(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	a Action,
) (*PreparedWorkflow, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := authorizeAction(ctx, tx, owner, a); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(a)
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	replay, found, err := replayAction(ctx, tx, owner, a.Key, hash)
	if err != nil {
		return nil, err
	}
	return &PreparedWorkflow{service: s, tx: tx, owner: owner, action: a, hash: hash, replay: replay, found: found}, nil
}

func (p *PreparedWorkflow) Replay() (Workflow, bool) { return p.replay, p.found }

// Apply does not commit; the application keeps its source locks through commit.
func (p *PreparedWorkflow) Apply(ctx context.Context) (Workflow, error) {
	if p.found {
		return p.replay, nil
	}
	s, tx, owner, a := p.service, p.tx, p.owner, p.action
	workflow, now, err := s.loadWorkflow(ctx, tx, owner, a.Version)
	if err != nil {
		return workflow, err
	}
	if err = s.applyAction(ctx, tx, &workflow, a, now); err != nil {
		return workflow, err
	}
	workflow.Version++
	if err = saveAction(ctx, tx, owner, a, workflow, p.hash); err != nil {
		return workflow, err
	}
	return workflow, nil
}
