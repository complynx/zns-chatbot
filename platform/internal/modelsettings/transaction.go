package modelsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const maxOperationKeyBytes = 128

type PreparedSet struct {
	tx           pgx.Tx
	actor, scope string
	input        Change
	request      []byte
	replay       State
	found        bool
}

func validChange(input Change) error {
	if !Valid(input.Selection) || input.Version < 0 || input.OperationKey == "" ||
		len(input.OperationKey) > maxOperationKeyBytes {
		return problem(http.StatusBadRequest, "invalid_model_settings")
	}
	return nil
}

func (s Service) PrepareSetInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, scope string,
	input Change,
) (*PreparedSet, error) {
	if err := validChange(input); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(481048)`); err != nil {
		return nil, err
	}
	if err := authorize(ctx, tx, actor, permission(actor, scope)); err != nil {
		return nil, err
	}
	if scope != GlobalScope {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, scope).
			Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, problem(http.StatusNotFound, "user_not_found")
		}
	}
	request, err := json.Marshal(struct {
		Change

		Scope string `json:"scope"`
	}{input, scope})
	if err != nil {
		return nil, err
	}
	previous, found, err := replay(ctx, tx, actor, input.OperationKey, request)
	if err != nil {
		return nil, err
	}
	return &PreparedSet{
		tx:      tx,
		actor:   actor,
		scope:   scope,
		input:   input,
		request: request,
		replay:  previous,
		found:   found,
	}, nil
}

func (p *PreparedSet) Replay() (State, bool) { return p.replay, p.found }

func (p *PreparedSet) Apply(ctx context.Context) (State, error) {
	if p.found {
		return p.replay, nil
	}
	state, err := read(ctx, p.tx, p.actor, p.scope)
	if err != nil {
		return State{}, err
	}
	if state.Version != p.input.Version {
		return State{}, problem(http.StatusConflict, "stale_model_settings")
	}
	if err = write(ctx, p.tx, p.actor, p.scope, p.input); err != nil {
		return State{}, err
	}
	state, err = read(ctx, p.tx, p.actor, p.scope)
	if err != nil {
		return State{}, err
	}
	result, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	_, err = p.tx.Exec(
		ctx,
		`INSERT INTO core.model_setting_operations(actor,operation_key,request,result) VALUES($1,$2,$3,$4)`,
		p.actor,
		p.input.OperationKey,
		p.request,
		result,
	)
	return state, err
}

type PreparedGrant struct {
	tx      pgx.Tx
	actor   string
	input   Grant
	request []byte
	found   bool
}

func validGrant(input Grant) error {
	if input.Capability != Own && input.Capability != Others && input.Capability != Global {
		return problem(http.StatusBadRequest, "invalid_capability")
	}
	if len(input.OperationKey) > maxOperationKeyBytes {
		return problem(http.StatusBadRequest, "invalid_operation_key")
	}
	return nil
}

func (s Service) PrepareGrantInTx(ctx context.Context, tx pgx.Tx, actor string, input Grant) (*PreparedGrant, error) {
	if err := validGrant(input); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(481048)`); err != nil {
		return nil, err
	}
	// Only canonical superadmins may grant; a saved receipt does not waive this.
	if err := authorize(ctx, tx, actor, ""); err != nil {
		return nil, err
	}
	request, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	p := &PreparedGrant{tx: tx, actor: actor, input: input, request: request}
	if input.OperationKey == "" {
		return p, nil
	}
	var same, ok bool
	err = tx.QueryRow(ctx, `SELECT request=$3::jsonb,(result->>'ok')::boolean FROM core.model_grant_operations WHERE actor=$1 AND operation_key=$2`, actor, input.OperationKey, request).
		Scan(&same, &ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if !same || !ok {
		return nil, problem(http.StatusConflict, "operation_conflict")
	}
	p.found = true
	return p, nil
}

func (p *PreparedGrant) Replay() bool { return p.found }

func (p *PreparedGrant) Apply(ctx context.Context) error {
	if p.found {
		return nil
	}
	var err error
	if p.input.Enabled {
		_, err = p.tx.Exec(
			ctx,
			`INSERT INTO core.model_setting_grants(owner,capability) VALUES($1,$2) ON CONFLICT DO NOTHING`,
			p.input.Owner,
			p.input.Capability,
		)
	} else {
		_, err = p.tx.Exec(
			ctx,
			`DELETE FROM core.model_setting_grants WHERE owner=$1 AND capability=$2`,
			p.input.Owner,
			p.input.Capability,
		)
	}
	if err != nil || p.input.OperationKey == "" {
		return err
	}
	_, err = p.tx.Exec(
		ctx,
		`INSERT INTO core.model_grant_operations(actor,operation_key,request,result) VALUES($1,$2,$3,'{"ok":true}')`,
		p.actor,
		p.input.OperationKey,
		p.request,
	)
	return err
}
