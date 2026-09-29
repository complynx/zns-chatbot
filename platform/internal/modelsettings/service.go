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
	OperationKey string `json:"operation_key,omitempty"`
	Owner        string `json:"owner"`
	Capability   string `json:"capability"`
	Enabled      bool   `json:"enabled"`
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
	if err := validGrant(input); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.PrepareGrantInTx(ctx, tx, actor, input)
	if err != nil {
		return err
	}
	if prepared.Replay() {
		return nil
	}
	if err = prepared.Apply(ctx); err != nil {
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
	if err := validChange(input); err != nil {
		return State{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.PrepareSetInTx(ctx, tx, actor, scope, input)
	if err != nil {
		return State{}, err
	}
	if value, found := prepared.Replay(); found {
		return value, nil
	}
	value, err := prepared.Apply(ctx)
	if err != nil {
		return value, err
	}
	return value, tx.Commit(ctx)
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
