package modelsettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type Service struct{ DB *pgxpool.Pool }
type State struct {
	Selection

	Version     int64           `json:"version"`
	Effective   Selection       `json:"effective"`
	Permissions map[string]bool `json:"permissions"`
	Catalog     []Option        `json:"catalog"`
}
type Change struct {
	Selection

	Version      int64  `json:"version"`
	OperationKey string `json:"operation_key"`
}
type Grant struct {
	Owner      string `json:"owner"`
	Capability string `json:"capability"`
	Enabled    bool   `json:"enabled"`
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func allowed(ctx context.Context, db querier, actor, capability string) (bool, error) {
	var result bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1)
 OR EXISTS(SELECT 1 FROM core.model_setting_grants WHERE owner=$1 AND capability=$2)`, actor, capability).Scan(&result)
	return result, err
}
func problem(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }
func permission(actor, scope string) string {
	if scope == GlobalScope {
		return Global
	}
	if scope == actor {
		return Own
	}
	return Others
}
func authorize(ctx context.Context, db querier, actor, capability string) error {
	ok, err := allowed(ctx, db, actor, capability)
	if err != nil {
		return err
	}
	if !ok {
		return problem(http.StatusForbidden, "forbidden")
	}
	return nil
}
func (s Service) Grant(ctx context.Context, actor string, input Grant) error {
	if input.Capability != Own && input.Capability != Others && input.Capability != Global {
		return problem(http.StatusBadRequest, "invalid_capability")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(481048)`); err != nil {
		return err
	}
	// An empty capability never matches a grant: only canonical superadmins qualify.
	if err = authorize(ctx, tx, actor, ""); err != nil {
		return err
	}
	if input.Enabled {
		_, err = tx.Exec(
			ctx,
			`INSERT INTO core.model_setting_grants(owner,capability) VALUES($1,$2) ON CONFLICT DO NOTHING`,
			input.Owner,
			input.Capability,
		)
	} else {
		_, err = tx.Exec(
			ctx,
			`DELETE FROM core.model_setting_grants WHERE owner=$1 AND capability=$2`,
			input.Owner,
			input.Capability,
		)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func effective(ctx context.Context, db querier, owner string) (Selection, error) {
	var selection Selection

	err := db.QueryRow(ctx, `SELECT m.model,m.effort FROM core.model_settings m WHERE m.model<>''
 AND (m.scope='*' OR (m.scope=$1 AND (m.authority='others' OR
 EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=$1) OR
 EXISTS(SELECT 1 FROM core.model_setting_grants g WHERE g.owner=$1 AND g.capability='own'))))
 ORDER BY (m.scope=$1) DESC LIMIT 1`, owner).Scan(&selection.Model, &selection.Effort)
	if errors.Is(err, pgx.ErrNoRows) {
		return Selection{Model: DefaultModel}, nil
	}
	return selection, err
}
func (s Service) Effective(ctx context.Context, owner string) (Selection, error) {
	return effective(ctx, s.DB, owner)
}
func read(ctx context.Context, db querier, actor, scope string) (State, error) {
	result := State{Permissions: map[string]bool{}, Catalog: Catalog()}
	for _, capability := range []string{Own, Others, Global, ""} {
		ok, err := allowed(ctx, db, actor, capability)
		if err != nil {
			return result, err
		}
		key := capability
		if key == "" {
			key = GrantPermission
		}
		result.Permissions[key] = ok
	}
	if !result.Permissions[permission(actor, scope)] {
		return State{}, problem(http.StatusForbidden, "forbidden")
	}
	err := db.QueryRow(ctx, `SELECT model,effort,version FROM core.model_settings WHERE scope=$1`, scope).
		Scan(&result.Model, &result.Effort, &result.Version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	result.Effective, err = effective(ctx, db, scope)
	return result, err
}
func (s Service) Read(ctx context.Context, actor, scope string) (State, error) {
	return read(ctx, s.DB, actor, scope)
}

func (s Service) Set(ctx context.Context, actor, scope string, input Change) (State, error) {
	if !Valid(input.Selection) || input.Version < 0 || input.OperationKey == "" || len(input.OperationKey) > 128 {
		return State{}, problem(http.StatusBadRequest, "invalid_model_settings")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(481048)`); err != nil {
		return State{}, err
	}
	if err = authorize(ctx, tx, actor, permission(actor, scope)); err != nil {
		return State{}, err
	}
	if scope != GlobalScope {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, scope).
			Scan(&exists); err != nil {
			return State{}, err
		}
		if !exists {
			return State{}, problem(http.StatusNotFound, "user_not_found")
		}
	}
	request, err := json.Marshal(struct {
		Change

		Scope string `json:"scope"`
	}{input, scope})
	if err != nil {
		return State{}, err
	}
	previous, replayed, err := replay(ctx, tx, actor, input.OperationKey, request)
	if err != nil || replayed {
		return previous, err
	}
	state, err := read(ctx, tx, actor, scope)
	if err != nil {
		return State{}, err
	}
	if state.Version != input.Version {
		return State{}, problem(http.StatusConflict, "stale_model_settings")
	}
	if err = write(ctx, tx, actor, scope, input); err != nil {
		return State{}, err
	}
	state, err = read(ctx, tx, actor, scope)
	if err != nil {
		return State{}, err
	}
	result, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.model_setting_operations(actor,operation_key,request,result) VALUES($1,$2,$3,$4)`,
		actor,
		input.OperationKey,
		request,
		result,
	)
	if err != nil {
		return State{}, err
	}
	return state, tx.Commit(ctx)
}

// Permissions returns only the caller's capabilities for host-owned menus.
func (s Service) Permissions(ctx context.Context, actor string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, capability := range []string{Own, Others, Global, ""} {
		ok, err := allowed(ctx, s.DB, actor, capability)
		if err != nil {
			return nil, err
		}
		key := capability
		if key == "" {
			key = GrantPermission
		}
		result[key] = ok
	}
	return result, nil
}

func replay(ctx context.Context, db querier, actor, key string, request []byte) (State, bool, error) {
	var prior, result []byte
	err := db.QueryRow(ctx, `SELECT request,result FROM core.model_setting_operations WHERE actor=$1 AND operation_key=$2`, actor, key).
		Scan(&prior, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var equal bool
	if err = db.QueryRow(ctx, `SELECT $1::jsonb=$2::jsonb`, prior, request).Scan(&equal); err != nil {
		return State{}, false, err
	}
	if !equal {
		return State{}, false, problem(http.StatusConflict, "operation_conflict")
	}
	var state State
	err = json.Unmarshal(result, &state)
	return state, true, err
}
